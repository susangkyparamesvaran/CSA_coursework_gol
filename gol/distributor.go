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
}

type workerResult struct {
	ID           int
	startY       int
	worldSection [][]byte
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
	world := make([][]byte, p.ImageHeight)
	for y := 0; y < p.ImageHeight; y++ {
		world[y] = make([]byte, p.ImageWidth)
		for x := 0; x < p.ImageWidth; x++ {
			world[y][x] = <-c.ioInput
		}
	}

	// Initialising mutexes: worldMu protects the world, turnMu protects the turn counter
	var worldMu sync.RWMutex
	var turnMu sync.RWMutex

	totalChunks := p.Threads * p.ChunksPerWorker
	sections := assignChunks(p.ImageHeight, totalChunks)

	jobChan := make(chan workerJob)
	resultChan := make(chan workerResult, len(sections))

	for i := 0; i < p.Threads; i++ {
		go worker(i, p, jobChan, resultChan)
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
				jobChan <- workerJob{startY: s.start, endY: s.end, world: world}
			}
		})

		// Collecting worker results
		results := make([]workerResult, 0, len(sections))
		for i := 0; i < len(sections); i++ {
			results = append(results, <-resultChan)
		}

		// Merging into the new world for the next turn
		newWorld := make([][]byte, p.ImageHeight)
		for i := range newWorld {
			newWorld[i] = make([]byte, p.ImageWidth)
		}

		for _, r := range results {
			for row := 0; row < len(r.worldSection); row++ {
				newWorld[r.startY+row] = r.worldSection[row]
			}
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

		writeLock(&worldMu, func() {
			world = newWorld
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
	close(jobChan)
}

func distributorDynamic(p Params, c distributorChannels, keypress <-chan rune) {

	//Load the input world
	filename := fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	// Read world into memory, builds a 2D world[][] array
	world := make([][]byte, p.ImageHeight)
	for y := 0; y < p.ImageHeight; y++ {
		world[y] = make([]byte, p.ImageWidth)
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

		// Each worker receives a chunk of rows
		resultChan := make(chan workerResult, len(sections))

		readLock(&worldMu, func() {
			for _, s := range sections {
				//spawn goroutine here
				go func(start, end int) {
					var next [][]byte
					if p.UsePrecomputed {
						next = calculateNextStatesPrecomputed(p, world, start, end)
					} else {
						next = calculateNextStatesInline(p, world, start, end)
					}

					sectionRows := next
					resultChan <- workerResult{
						startY:       start,
						worldSection: sectionRows,
					}
				}(s.start, s.end)

			}
		})

		// Collecting worker results
		results := make([]workerResult, 0, len(sections))
		for i := 0; i < len(sections); i++ {
			results = append(results, <-resultChan)
		}

		close(resultChan)

		// Merging into the new world for the next turn
		newWorld := make([][]byte, p.ImageHeight)
		for i := range newWorld {
			newWorld[i] = make([]byte, p.ImageWidth)
		}

		for _, r := range results {
			for row := 0; row < len(r.worldSection); row++ {
				newWorld[r.startY+row] = r.worldSection[row]
			}
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

		writeLock(&worldMu, func() {
			world = newWorld
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
func worker(id int, p Params, jobs <-chan workerJob, results chan<- workerResult) {
	for job := range jobs {
		var next [][]byte
		if p.UsePrecomputed {
			next = calculateNextStatesPrecomputed(p, job.world, job.startY, job.endY)
		} else {
			next = calculateNextStatesInline(p, job.world, job.startY, job.endY)
		}
		results <- workerResult{
			ID:           id,
			startY:       job.startY,
			worldSection: next,
		}
	}
}

// Computes the next state of one horizontal section of GOL world
func calculateNextStatesPrecomputed(p Params, world [][]byte, startY, endY int) [][]byte {
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

func calculateNextStatesInline(p Params, world [][]byte, startY, endY int) [][]byte {
	h := p.ImageHeight
	w := p.ImageWidth

	rows := endY - startY
	newRows := make([][]byte, rows)
	for i := 0; i < rows; i++ {
		newRows[i] = make([]byte, w)
	}

	for i := startY; i < endY; i++ {
		for j := 0; j < w; j++ {

			// Wrap-around (branchy but cheaper than %)
			up := i - 1
			if up < 0 {
				up = h - 1
			}
			down := i + 1
			if down == h {
				down = 0
			}

			left := j - 1
			if left < 0 {
				left = w - 1
			}
			right := j + 1
			if right == w {
				right = 0
			}

			row := world[i]
			upRow := world[up]
			downRow := world[down]

			// Branchless neighbour count (0 or 1 per cell)
			count := 0
			count += int(row[left] >> 7)
			count += int(row[right] >> 7)
			count += int(upRow[j] >> 7)
			count += int(downRow[j] >> 7)
			count += int(upRow[left] >> 7)
			count += int(upRow[right] >> 7)
			count += int(downRow[left] >> 7)
			count += int(downRow[right] >> 7)

			// Use helper — cleaner + reduces branches
			newRows[i-startY][j] = nextCellState(world[i][j], count)
		}
	}

	return newRows
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

