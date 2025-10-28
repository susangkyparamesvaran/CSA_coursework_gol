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

	turn := 0
	c.events <- StateChange{turn, Executing}

	// TODO: Execute all turns of the Game of Life.
	for turn = 0; turn < p.Turns; turn++ {
		world = calculateNextStates(p, world)
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
