package main

import (
	"log"
	"math"
	"time"

	pb "server/proto"
)

// Physics/game loop
func (s *CarServer) physicsLoop() {
	ticker := time.NewTicker(updateRate)
	defer ticker.Stop()
	last := time.Now()

	for range ticker.C {
		now := time.Now()
		dt := float32(now.Sub(last).Seconds())
		last = now

		s.mu.Lock()
		s.gameTick++

		// Update race time for time-based races
		if s.raceType == pb.RaceType_RACEBYTIME {
			s.raceTimeLeft = int32(raceTime - int(now.Sub(s.raceStarted).Seconds()))
			if s.raceTimeLeft <= 0 {
				s.raceTimeLeft = 0
				s.raceStatus.Status = "finished"
			}
		}

		var maxLap int32 = 0
		var maxProgress float32 = 0

		for _, car := range s.carInfos {
			state := s.carStates[car.carId]
			input := s.playerInput[car.carId]

			// Update penalty timers
			if penalty, hasPenalty := s.penalties[car.carId]; hasPenalty {
				penalty.RemainingPenalty -= int32(dt * 1000) // Convert to milliseconds
				if penalty.RemainingPenalty <= 0 {
					delete(s.penalties, car.carId)
					state.Status = pb.CarStatus_RACING
				} else {
					state.Status = pb.CarStatus_SERVINGPENALTY
				}
			}

			// Only update physics if car is racing (not serving penalty or finished)
			if state.Status == pb.CarStatus_RACING {
				s.updateCarPhysics(state, input, dt, now)

				// Determine leader (by lap and progress along track)
				progress := s.calculateTrackProgress(state.Position)
				if state.Lap > maxLap || (state.Lap == maxLap && progress > maxProgress) {
					maxLap = state.Lap
					maxProgress = progress
				}

				// Check if finished (for lap-based races)
				if s.raceType == pb.RaceType_RACEBYLAPS && state.Lap >= s.raceLaps {
					state.Status = pb.CarStatus_FINISHED
				}

				// For time-based races, finish when time runs out
				if s.raceType == pb.RaceType_RACEBYTIME && s.raceTimeLeft <= 0 {
					state.Status = pb.CarStatus_FINISHED
				}
			}
		}

		s.raceStatus.GameTick = s.gameTick

		// Check if race is finished
		if s.raceType == pb.RaceType_RACEBYLAPS && maxLap >= s.raceLaps {
			s.raceStatus.Status = "finished"
		}

		// Create update
		update := s.createRaceUpdate()
		for clientChan := range s.clients {
			select {
			case clientChan <- update:
			default:
			}
		}

		s.mu.Unlock()
	}
}

// Physics for each car
func (s *CarServer) updateCarPhysics(state *CarStateExtended, input *PlayerInput, dt float32, now time.Time) {
	// Apply acceleration/brake
	if input.throttle > 0 {
		state.Speed += acceleration * input.throttle * dt
	} else if input.brake > 0 {
		state.Speed -= brakeForce * input.brake * dt
	} else {
		state.Speed -= friction * dt
	}

	if state.Speed < 0 {
		state.Speed = 0
	}
	if state.Speed > maxSpeed {
		state.Speed = maxSpeed
	}

	// Steering
	if state.Speed > 10 && input.steering != 0 {
		turnRate := turnSpeed * (state.Speed / maxSpeed)
		state.Heading += turnRate * input.steering * dt
		for state.Heading < 0 {
			state.Heading += 360
		}
		for state.Heading >= 360 {
			state.Heading -= 360
		}
	}

	rad := float64(state.Heading) * math.Pi / 180
	dx := float32(math.Cos(rad)) * state.Speed * dt
	dy := float32(math.Sin(rad)) * state.Speed * dt

	state.Position.X += dx
	state.Position.Y += dy

	// Lap detection
	currentProgress := s.calculateTrackProgress(state.Position)

	// Detect crossing finish line (progress wraps from ~1.0 to ~0.0)
	if state.lastProgress > 0.9 && currentProgress < 0.1 && state.Speed > 0 {
		state.Lap++

		// Record lap time
		lapTime := float32(now.Sub(state.currentLapStart).Seconds())
		state.lapTimes = append(state.lapTimes, lapTime)

		// Update best lap
		if state.bestLapTime == 0 || lapTime < state.bestLapTime {
			state.bestLapTime = lapTime
		}

		state.currentLapStart = now
		log.Printf("Car %s completed lap %d in %.2fs (best: %.2fs)",
			state.CarId, state.Lap, lapTime, state.bestLapTime)
	}

	state.lastProgress = currentProgress
}
