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
	dest   [][]byte // destination buffer (subset of nextWorld)
	world  [][]byte
}

type workerJobInline struct {
	startY int
	endY   int
	dest   [][]byte // destination buffer (subset of nextWorld)
	world  [][]byte
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
	// defer schedules a statement to run after the surrounding function returns
	//acquire lock -> schedule the lock -> run func() -> when readLock finished, mutexVar.RUnlock() auto runs
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
	return *turn //dereference the pointer and return the value stored at that memory location
}

// Safely modify the turn counter
func setTurn(mu *sync.RWMutex, turn *int, v int) {
	mu.Lock()
	defer mu.Unlock()
	*turn = v
}

// Scans through every cell in the world and calls a call back function executeFunction(...) for each cell
func scanWorld(world [][]byte, width, height int, executeFunction func(x, y int, alive bool)) {
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			executeFunction(x, y, world[y][x] == 255)
		}
	}
}

// Static array for the 8 directions of neighbours around a cell
var neighbourOffsets = [8][2]int{
	{-1, -1}, {-1, 0}, {-1, 1},
	{0, -1}, {0, 1},
	{1, -1}, {1, 0}, {1, 1},
}

// Count how many of the cell's 8 neighbours are alive
func countNeighbours(world [][]byte, x, y, width, height int) int {
	count := 0
	for _, offset := range neighbourOffsets {
		// Computes neighbour coordinates using wrap-around
		ny := (y + offset[0] + height) % height
		nx := (x + offset[1] + width) % width
		if world[ny][nx] == 255 {
			count++
		}
	}
	return count
}

// Applies the GOL rules to decide if a cell will be alive or dead in the next generation
func nextCellState(currCellValue byte, neighbours int) byte {
	alive := currCellValue == 255
	switch {
	case alive && (neighbours == 2 || neighbours == 3):
		return 255
	case !alive && neighbours == 3:
		return 255
	default:
		return 0
	}
}

// /////////////////////////////////////////////////////////////////////////
// DISTRIBUTOR
// /////////////////////////////////////////////////////////////////////////

func distributor(p Params, c distributorChannels, keypress <-chan rune) {

	//Load the input world
	filename := fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	// Read world into memory, builds a 2D world[][] array
	// Allocate TWO full boards once
	world := make([][]byte, p.ImageHeight)
	newWorld := make([][]byte, p.ImageHeight)

	for y := 0; y < p.ImageHeight; y++ {
		world[y] = make([]byte, p.ImageWidth)
		newWorld[y] = make([]byte, p.ImageWidth)
		for x := 0; x < p.ImageWidth; x++ {
			world[y][x] = <-c.ioInput
		}
	}

	// Initialising mutexes: worldMu protects the world, turnMu protects the turn counter
	var worldMu sync.RWMutex
	var turnMu sync.RWMutex

	var totalChunks int

	if p.ChunksPerWorker == 0 {
		totalChunks = 1
	} else {
		totalChunks = p.Threads * p.ChunksPerWorker
	}

	sections := assignChunks(p.ImageHeight, totalChunks)

	jobChan := make(chan workerJob)
	jobChanInline := make(chan workerJobInline)
	resultChan := make(chan int, len(sections))

	for i := 0; i < p.Threads; i++ {
		if p.UsePrecomputed {
			go worker(i, p, jobChan, resultChan)
		} else {
			go workerInline(i, p, jobChanInline, resultChan)
		}

	}

	// Starting the ticker goroutine for every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	done := make(chan bool)

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

				// Send AliveCellsCount event to SDL
				c.events <- AliveCellsCount{
					CompletedTurns: getTurn(&turnMu, &turn),
					CellsCount:     count,
				}

			case <-done:
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

	w := p.ImageWidth
	h := p.ImageHeight

	left := make([]int, w)
	right := make([]int, w)
	for x := 0; x < w; x++ {
		left[x] = x - 1
		if left[x] < 0 {
			left[x] = w - 1
		}
		right[x] = x + 1
		if right[x] == w {
			right[x] = 0
		}
	}

	up := make([]int, h)
	down := make([]int, h)
	for y := 0; y < h; y++ {
		up[y] = y - 1
		if up[y] < 0 {
			up[y] = h - 1
		}
		down[y] = y + 1
		if down[y] == h {
			down[y] = 0
		}
	}

	// /////////////////////////////////////////////////////////////////////////
	// MAIN LOOP
	// /////////////////////////////////////////////////////////////////////////

	for {
		select {
		//Checking for keypress
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

		// Each worker receives a chunk of rows
		readLock(&worldMu, func() {
			for _, s := range sections {
				if p.UsePrecomputed {
					jobChan <- workerJob{
						startY: s.start,
						endY:   s.end,
						world:  world,
						dest:   newWorld[s.start:s.end],
					}
				} else {
					jobChanInline <- workerJobInline{
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
			}
		})

		// Collecting worker results
		for i := 0; i < len(sections); i++ {
			<-resultChan // just wait for each worker chunk
		}

		// Comparing old and new world
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

		if p.UseOptimised {
			// FAST: reuse buffers (swap)
			writeLock(&worldMu, func() {
				world, newWorld = newWorld, world
			})
		} else {
			// BASELINE: reallocate newWorld every turn
			temp := make([][]byte, p.ImageHeight)
			for i := range temp {
				temp[i] = make([]byte, p.ImageWidth)
			}

			// copy next state into temp
			for y := 0; y < p.ImageHeight; y++ {
				copy(temp[y], newWorld[y])
			}

			writeLock(&worldMu, func() {
				world = temp
			})
		}

		setTurn(&turnMu, &turn, getTurn(&turnMu, &turn)+1)

		c.events <- TurnComplete{CompletedTurns: getTurn(&turnMu, &turn)}
	}

	// Stopping the ticker
	done <- true
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
	if p.UsePrecomputed {
		close(jobChan)
	} else {
		close(jobChanInline)
	}
}

func distributorDynamic(p Params, c distributorChannels, keypress <-chan rune) {

	// Load the input world
	filename := fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	// Read world into memory, builds a 2D world[][] array
	world := make([][]byte, p.ImageHeight)
	newWorld := make([][]byte, p.ImageHeight) // second buffer for double-buffering
	for y := 0; y < p.ImageHeight; y++ {
		world[y] = make([]byte, p.ImageWidth)
		newWorld[y] = make([]byte, p.ImageWidth)
		for x := 0; x < p.ImageWidth; x++ {
			world[y][x] = <-c.ioInput
		}
	}

	// Initialising mutexes: worldMu protects the world, turnMu protects the turn counter
	var worldMu sync.RWMutex
	var turnMu sync.RWMutex

	// Starting the ticker goroutine for every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	done := make(chan bool)

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

				// Send AliveCellsCount event to SDL
				c.events <- AliveCellsCount{
					CompletedTurns: getTurn(&turnMu, &turn),
					CellsCount:     count,
				}

			case <-done:
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

	sections := assignSections(p.ImageHeight, p.Threads)

	paused := false
	quitting := false

	// /////////////////////////////////////////////////////////////////////////
	// MAIN LOOP
	// /////////////////////////////////////////////////////////////////////////

	for {
		select {
		//Checking for keypress
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

		// Dynamic: spawn fresh goroutines every turn,
		// but write into preallocated newWorld instead of allocating.
		resultChan := make(chan int, len(sections))

		readLock(&worldMu, func() {
			for _, s := range sections {
				start, end := s.start, s.end
				go func(start, end int) {
					calculateNextStatesPrecomputed(p,
						world,
						newWorld[start:end],
						start,
						end,
					)
					resultChan <- start // just signal completion
				}(start, end)
			}
		})

		// Wait for all dynamic workers to finish
		for i := 0; i < len(sections); i++ {
			<-resultChan
		}
		close(resultChan)

		// Comparing old and new world
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

		// Swap buffers: newWorld becomes current, old world reused as next buffer
		writeLock(&worldMu, func() {
			world, newWorld = newWorld, world
		})

		setTurn(&turnMu, &turn, getTurn(&turnMu, &turn)+1)

		c.events <- TurnComplete{CompletedTurns: getTurn(&turnMu, &turn)}
	}

	// Stopping the ticker
	done <- true
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
}

// /////////////////////////////////////////////////////////////////////////
// WORKER + UTILS
// /////////////////////////////////////////////////////////////////////////

// Function run by each worker goroutine, forms the worker pool
func worker(id int, p Params, jobs <-chan workerJob, done chan<- int) {
	for job := range jobs {
		// old non-optimised behaviour: create fresh rows
		next := calculateNextStatesPrecomputed(p, job.world, job.dest, job.startY, job.endY)
		for i := range next {
			copy(job.dest[i], next[i])
		}
		done <- job.startY // notify completion of this chunk
	}
}

func workerInline(id int, p Params, jobs <-chan workerJobInline, done chan<- int) {
	for job := range jobs {
		if p.UseOptimised {
			// write into dest (preallocated)
			calculateNextStatesInline(p, job.world, job.dest, job.startY, job.endY, job.left, job.right, job.up, job.down)
		} else {
			// old non-optimised behaviour: create fresh rows
			next := calculateNextStatesPrecomputed(p, job.world, job.dest, job.startY, job.endY)
			for i := range next {
				copy(job.dest[i], next[i])
			}
		}
		done <- job.startY // notify completion of this chunk
	}
}

// Computes the next state of one horizontal section of GOL world
func calculateNextStatesPrecomputed(p Params, world [][]byte, dest [][]byte, startY, endY int) [][]byte {
	h := p.ImageHeight
	w := p.ImageWidth

	rows := endY - startY
	newRows := make([][]byte, rows)

	for i := 0; i < rows; i++ {
		newRows[i] = make([]byte, w)
	}

	for y := startY; y < endY; y++ {
		for x := 0; x < w; x++ {
			n := countNeighbours(world, x, y, w, h)
			newRows[y-startY][x] = nextCellState(world[y][x], n)
		}
	}

	return newRows
}

func calculateNextStatesInline(p Params, world [][]byte, dest [][]byte, startY, endY int, left, right, up, down []int) {
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

			// branchless neighbour count
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

			// Apply GOL rules quickly using integer operations
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

func assignChunks(height, totalChunks int) []section {
	minRowsPerChunk := height / totalChunks
	extra := height % totalChunks

	sections := make([]section, totalChunks)
	start := 0

	for i := 0; i < totalChunks; i++ {
		rows := minRowsPerChunk
		if i < extra {
			rows++
		}
		end := start + rows
		sections[i] = section{start, end}
		start = end
	}

	return sections
}
