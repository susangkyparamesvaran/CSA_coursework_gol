package gol

import (
	"fmt"

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

// structs for our channels used to communicate with the worker goroutine
type workerJob struct {
	startY int
	endY   int
	world  [][]byte
}

type workerResult struct {
	ID           int
	worldSection [][]byte
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {

	// TODO: Create a 2D slice to store the world.

	filename := fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	world := make([][]byte, p.ImageHeight)
	for y := 0; y < p.ImageHeight; y++ {
		world[y] = make([]byte, p.ImageWidth)
		for x := 0; x < p.ImageWidth; x++ {
			world[y][x] = <-c.ioInput
		}
	}

	// Channels to send work and receive results
	jobChan := make(chan workerJob)
	resultChan := make(chan workerResult)

	// Start ticker to report alive cells every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	//Channel used to sognal the goroutine to stop
	done := make(chan bool)

	go func() {
		for {
			select {
			// Case runs every time the timer ticks (every 2 seconds)
			case <-ticker.C:
				aliveCount := 0

				//loop to count alive cells
				for y := 0; y < p.ImageHeight; y++ {
					for x := 0; x < p.ImageWidth; x++ {
						if world[y][x] == 255 {
							aliveCount++
						}
					}
				}
				c.events <- AliveCellsCount{
					CompletedTurns: turn,
					CellsCount:     aliveCount,
				}
			case <-done:
				return
			}
		}
	}()

	turn := 0
	c.events <- StateChange{turn, Executing}

	/////// CHECK THIS PART  /////////////////
	// Assigning rows to each worker evenly
	// say if we had 16 rows and 4 threads
	// we want to be able to allocate say 4 rows to 1 thread, 4 to the other thread etc.
	workers := p.Threads

	// we need to calculate the minimum number of rows for each worker
	minRows := p.ImageHeight / p.Threads
	// then say if we have extra rows left over then we need to assign those evenly to each worker
	extraRows := p.ImageHeight % p.Threads
	
	// make a slice, the size of the number of threads
	sections := make([]section, workers)
	start := 0
	// for each worker
	for i := 0; i < workers; i ++ {
		// assigns the base amount of rows to the thread
		rows := minRows
		// if say we're on worker 2 and there are 3 extra rows left, 
		// then we can add 1 more job to the thread
		if i < extraRows {
			rows ++
		}

		// marks where the end of the section ends
		end := start + rows
		// assigns these rows to the section
		sections[i] = section{start : start, end : end}
		// start is updated for the next worker
		start = end
	}

	/////////////////////////////////////////////////////////////////////

	//starting goroutines
	for i := 0; i < p.Threads; i++ {
		go worker(i, p, jobChan, resultChan)
	}

	// TODO: Execute all turns of the Game of Life.
	for turn = 0; turn < p.Turns; turn++ {
		world = calculateNextStates(p, world)
	}

	// Stop ticker after finishing all turns
	done <- true
	ticker.Stop()

	// Write final world to output file (PGM)
	// Construct the output filename in the required format
	// Example: "512x512x100" for a 512x512 world after 100 turns
	outFileName := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, p.Turns)
	c.ioCommand <- ioOutput     // telling the i/o goroutine that we are starting an output operation
	c.ioFilename <- outFileName // sending the filename to io goroutine

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			//writing the pixel value to the ioOutput channel
			c.ioOutput <- world[y][x] //grayscale value for that pixel (0 or 255)
		}
	}

	// TODO: Report the final state using FinalTurnCompleteEvent.
	alive_cells := AliveCells(world, p.ImageWidth, p.ImageHeight)
	c.events <- FinalTurnComplete{
		CompletedTurns: p.Turns,
		Alive:          alive_cells,
	}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}

func calculateNextStates(p Params, world [][]byte) [][]byte {
	h := p.ImageHeight //h rows
	w := p.ImageWidth  //w columns

	//make new grid
	newWorld := make([][]byte, h)
	for i := 0; i < h; i++ {
		newWorld[i] = make([]byte, w)
	}

	for i := 0; i < h; i++ {
		for j := 0; j < w; j++ { //accessing each individual cell
			count := 0
			up := (i - 1 + h) % h
			down := (i + 1) % h
			left := (j - 1 + w) % w
			right := (j + 1) % w

			//need to check all it's neighbors and state of it's cell
			leftCell := world[i][left]
			if leftCell == 255 {
				count += 1
			}
			rightCell := world[i][right]
			if rightCell == 255 {
				count += 1
			}
			upCell := world[up][j]
			if upCell == 255 {
				count += 1
			}
			downCell := world[down][j]
			if downCell == 255 {
				count += 1
			}
			upRightCell := world[up][right]
			if upRightCell == 255 {
				count += 1
			}
			upLeftCell := world[up][left]
			if upLeftCell == 255 {
				count += 1
			}

			downRightCell := world[down][right]
			if downRightCell == 255 {
				count += 1
			}

			downLeftCell := world[down][left]
			if downLeftCell == 255 {
				count += 1
			}

			//update the cells
			if world[i][j] == 255 {
				if count == 2 || count == 3 {
					newWorld[i][j] = 255
				} else {
					newWorld[i][j] = 0
				}
			}

			if world[i][j] == 0 {
				if count == 3 {
					newWorld[i][j] = 255
				} else {
					newWorld[i][j] = 0
				}
			}

		}
	}
	return newWorld
}

func AliveCells(world [][]byte, width, height int) []util.Cell {
	cells := make([]util.Cell, 0)
	for dy := 0; dy < height; dy++ {
		for dx := 0; dx < width; dx++ {
			if world[dy][dx] == 255 {
				cells = append(cells, util.Cell{X: dx, Y: dy})
			}
		}
	}
	return cells
}


func worker(id int, p Params, jobs <-chan workerJob, results chan<- workerResult) {
	width := p.ImageWidth
	height := p.ImageHeight


	for job := range jobs {
		start := job.startY
		end := job.endY
		world := job.world

		length := end - start
		// allocate a slice for one thread for what rows it needs ot compute
		AllocatedSection := make([][]byte, length)
		for row := range AllocatedSection {
			AllocatedSection[row] = make([]byte, width)
		}

		// loop through each row for each thread
		for y:= start; y < end; y ++ {
			// calculateNextState?
		}

		results <- // idk smth here
	}


}




func worker(id int, p Params, jobs <-chan workerJob, results chan<- workerResult) {

}
