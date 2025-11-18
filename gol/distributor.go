package gol

import (
	"fmt"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

type workerJob struct {
	startY int
	endY   int
	world  [][]byte
	dest   [][]byte
	left   []int
	right  []int
	up     []int
	down   []int
}

type section struct {
	start, end int
}

// /////////////////////////////////////////////////////////////////////////
// HELPER FUNCTIONS
// /////////////////////////////////////////////////////////////////////////

// Runs the function executeFunction() while holding a read lock
func readLock(mutexVar *sync.RWMutex, executeFunction func()) {
	mutexVar.RLock()
	defer mutexVar.RUnlock()
	executeFunction()
}

// Runs the function executeFunction() with exclusive write access
func writeLock(mutexVar *sync.RWMutex, executeFunction func()) {
	mutexVar.Lock()
	defer mutexVar.Unlock()
	executeFunction()
}

// Safely reads the integer turn shared between goroutines
func getTurn(mu *sync.RWMutex, turn *int) int {
	mu.RLock()
	defer mu.RUnlock()
	return *turn
}

// Safely modify the turn counter
func setTurn(mu *sync.RWMutex, turn *int, v int) {
	mu.Lock()
	defer mu.Unlock()
	*turn = v
}

// Scans through every cell in the world and calls executeFunction(...) for each cell
func scanWorld(world [][]byte, width, height int, executeFunction func(x, y int, alive bool)) {
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			executeFunction(x, y, world[y][x] == 255)
		}
	}
}

// /////////////////////////////////////////////////////////////////////////
// DISTRIBUTOR (optimised)
// /////////////////////////////////////////////////////////////////////////

func distributor(p Params, c distributorChannels, keypress <-chan rune) {

	// Load the input world
	filename := fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	// Allocate TWO full boards once: world + newWorld
	world := make([][]byte, p.ImageHeight)
	newWorld := make([][]byte, p.ImageHeight)

	for y := 0; y < p.ImageHeight; y++ {
		world[y] = make([]byte, p.ImageWidth)
		newWorld[y] = make([]byte, p.ImageWidth)
		for x := 0; x < p.ImageWidth; x++ {
			world[y][x] = <-c.ioInput
		}
	}

	// Mutexes: worldMu protects the world pointer, turnMu protects the turn counter
	var worldMu sync.RWMutex
	var turnMu sync.RWMutex

	// Precompute wrap-around indices once (no % in inner loop)
	w := p.ImageWidth
	h := p.ImageHeight

	left := make([]int, w)
	right := make([]int, w)
	for x := 0; x < w; x++ {
		l := x - 1
		if l < 0 {
			l = w - 1
		}
		r := x + 1
		if r == w {
			r = 0
		}
		left[x] = l
		right[x] = r
	}

	up := make([]int, h)
	down := make([]int, h)
	for y := 0; y < h; y++ {
		u := y - 1
		if u < 0 {
			u = h - 1
		}
		d := y + 1
		if d == h {
			d = 0
		}
		up[y] = u
		down[y] = d
	}

	sections := assignSections(p.ImageHeight, p.Threads)

	jobChan := make(chan workerJob)
	doneChan := make(chan int, len(sections))

	// Persistent worker pool
	for i := 0; i < p.Threads; i++ {
		go worker(i, p, jobChan, doneChan)
	}

	// Starting the ticker goroutine for every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	doneTicker := make(chan bool)

	turn := 0

	// /////////////////////////////////////////////////////////////////////////
	// TICKER GOROUTINE
	// /////////////////////////////////////////////////////////////////////////

	go func() {
		for {
			select {
			case <-ticker.C:
				var count int
				// Count alive cells safely
				readLock(&worldMu, func() {
					scanWorld(world, p.ImageWidth, p.ImageHeight,
						func(_, _ int, alive bool) {
							if alive {
								count++
							}
						})
				})

				c.events <- AliveCellsCount{
					CompletedTurns: getTurn(&turnMu, &turn),
					CellsCount:     count,
				}

			case <-doneTicker:
				return
			}
		}
	}()

	// Compute initial alive cells
	initialAlive := make([]util.Cell, 0)
	scanWorld(world, p.ImageWidth, p.ImageHeight, func(x, y int, alive bool) {
		if alive {
			initialAlive = append(initialAlive, util.Cell{X: x, Y: y})
		}
	})

	if len(initialAlive) > 0 {
		c.events <- CellsFlipped{CompletedTurns: 0, Cells: initialAlive}
	}

	// Notify SDL that execution has started
	c.events <- StateChange{turn, Executing}

	paused := false
	quitting := false

	// /////////////////////////////////////////////////////////////////////////
	// MAIN LOOP
	// /////////////////////////////////////////////////////////////////////////

	for {
		select {
		case key := <-keypress:
			switch key {
			case 'p':
				paused = !paused
				state := Paused
				if !paused {
					state = Executing
				}
				c.events <- StateChange{getTurn(&turnMu, &turn), state}

			case 's':
				readLock(&worldMu, func() {
					saveImage(p, c, world, getTurn(&turnMu, &turn))
				})

			case 'q':
				quitting = true
			}
			continue
		default:
		}

		if quitting || (getTurn(&turnMu, &turn) >= p.Turns && !paused) {
			break
		}

		if paused {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Dispatch one job per section, workers write directly into newWorld
		readLock(&worldMu, func() {
			for _, s := range sections {
				jobChan <- workerJob{
					startY: s.start,
					endY:   s.end,
					world:  world,
					dest:   newWorld[s.start:s.end],
					left:   left,
					right:  right,
					up:     up,
					down:   down,
				}
			}
		})

		// Wait for all workers to finish their chunks
		for i := 0; i < len(sections); i++ {
			<-doneChan
		}

		// Compare old world vs newWorld to find flipped cells
		flipped := make([]util.Cell, 0)

		readLock(&worldMu, func() {
			scanWorld(world, p.ImageWidth, p.ImageHeight,
				func(x, y int, _ bool) {
					if world[y][x] != newWorld[y][x] {
						flipped = append(flipped, util.Cell{X: x, Y: y})
					}
				})
		})

		if len(flipped) > 0 {
			c.events <- CellsFlipped{
				CompletedTurns: getTurn(&turnMu, &turn) + 1,
				Cells:          flipped,
			}
		}

		// Constant-time pointer swap- reuse both boards, no per-turn allocation
		writeLock(&worldMu, func() {
			world, newWorld = newWorld, world
		})

		setTurn(&turnMu, &turn, getTurn(&turnMu, &turn)+1)
		c.events <- TurnComplete{CompletedTurns: getTurn(&turnMu, &turn)}
	}

	// Stopping ticker
	doneTicker <- true
	ticker.Stop()

	var finalAlive []util.Cell
	readLock(&worldMu, func() {
		finalAlive = []util.Cell{}
		scanWorld(world, p.ImageWidth, p.ImageHeight, func(x, y int, alive bool) {
			if alive {
				finalAlive = append(finalAlive, util.Cell{X: x, Y: y})
			}
		})
	})

	c.events <- FinalTurnComplete{
		CompletedTurns: getTurn(&turnMu, &turn),
		Alive:          finalAlive,
	}

	readLock(&worldMu, func() {
		saveImage(p, c, world, getTurn(&turnMu, &turn))
	})

	c.events <- StateChange{getTurn(&turnMu, &turn), Quitting}

	close(c.events)
	close(jobChan)
}

// /////////////////////////////////////////////////////////////////////////
// WORKER + UTILS
// /////////////////////////////////////////////////////////////////////////

func worker(id int, p Params, jobs <-chan workerJob, done chan<- int) {
	for job := range jobs {
		calculateNextStatesInline(
			p,
			job.world,
			job.dest,
			job.startY,
			job.endY,
			job.left,
			job.right,
			job.up,
			job.down,
		)
		done <- job.startY
	}
}

// Branchless, no-modulo neighbour counting for one horizontal section
func calculateNextStatesInline(p Params, world [][]byte, dest [][]byte, startY, endY int,
	left, right, up, down []int) {

	w := p.ImageWidth

	for y := startY; y < endY; y++ {

		row := world[y]
		rowUp := world[up[y]]
		rowDown := world[down[y]]

		ny := y - startY
		dstRow := dest[ny]

		for x := 0; x < w; x++ {
			lx := left[x]
			rx := right[x]

			// Branchless neighbour count: 0/255 → 0/1 via >> 7
			count :=
				int(row[lx]>>7) +
					int(row[rx]>>7) +
					int(rowUp[x]>>7) +
					int(rowDown[x]>>7) +
					int(rowUp[lx]>>7) +
					int(rowUp[rx]>>7) +
					int(rowDown[lx]>>7) +
					int(rowDown[rx]>>7)

			alive := row[x] >> 7

			// Apply Game of Life rules
			if alive == 1 {
				if count == 2 || count == 3 {
					dstRow[x] = 255
				} else {
					dstRow[x] = 0
				}
			} else {
				if count == 3 {
					dstRow[x] = 255
				} else {
					dstRow[x] = 0
				}
			}
		}
	}
}

// Writes the current world state into a PGM output file through the I/O goroutine
func saveImage(p Params, c distributorChannels, world [][]byte, turn int) {
	out := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, turn)

	c.ioCommand <- ioOutput
	c.ioFilename <- out

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- world[y][x]
		}
	}

	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- ImageOutputComplete{CompletedTurns: turn, Filename: out}
}

// Divides the world into evenly sized subsections so each worker thread receives a fair workload
func assignSections(height, threads int) []section {
	minRows := height / threads
	extra := height % threads

	sections := make([]section, threads)
	start := 0

	for i := 0; i < threads; i++ {
		rows := minRows
		if i < extra {
			rows++
		}
		end := start + rows
		sections[i] = section{start, end}
		start = end
	}

	return sections
}
