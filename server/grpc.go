package main

import (
	"context"
	pb "server/proto"
)

// Stream race updates
func (s *CarServer) StreamRaceUpdates(req *pb.Empty, stream pb.CarService_StreamRaceUpdatesServer) error {
	clientChan := make(chan *pb.RaceUpdate, 10)

	s.mu.Lock()
	s.clients[clientChan] = struct{}{}
	s.mu.Unlock()

	// Stream updates
	defer func() {
		s.mu.Lock()
		delete(s.clients, clientChan)
		close(clientChan)
		s.mu.Unlock()
	}()

	for update := range clientChan {
		if err := stream.Send(update); err != nil {
			return err
		}
	}
	return nil
}

func (s *CarServer) createRaceUpdate() *pb.RaceUpdate {
	states := make([]*pb.CarState, 0, len(s.carStates))
	for _, state := range s.carStates {
		states = append(states, &pb.CarState{
			CarId:  state.CarId,
			Status: state.Status,
			Position: &pb.Point3D{
				X: state.Position.X,
				Y: state.Position.Y,
				Z: state.Position.Z,
			},
			Heading: state.Heading,
			Speed:   state.Speed,
			Lap:     state.Lap,
		})
	}

	penalties := make([]*pb.CarPenalty, 0, len(s.penalties))
	for _, penalty := range s.penalties {
		penalties = append(penalties, &pb.CarPenalty{
			CarId:            penalty.CarId,
			Reason:           penalty.Reason,
			GameTick:         penalty.GameTick,
			RemainingPenalty: penalty.RemainingPenalty,
		})
	}

	// Calculate intervals
	toLeader, forPosition := s.calculateIntervals()

	return &pb.RaceUpdate{
		RaceStatus: &pb.RaceStatus{
			Status:    s.raceStatus.Status,
			TotalLaps: s.raceStatus.TotalLaps,
			GameTick:  s.raceStatus.GameTick,
		},
		Cars:        states,
		Penalties:   penalties,
		ToLeader:    toLeader,
		ForPosition: forPosition,
		GameTick:    s.gameTick,
	}
}

// Unary RPC for per-frame input
func (s *CarServer) SendPlayerInput(ctx context.Context, input *pb.PlayerInput) (*pb.InputAck, error) {
	carId := input.GetCarId()
	if !s.validateToken(carId, input.GetAuthToken()) {
		return &pb.InputAck{
			Accepted: false,
			Reason:   "invalid token",
			GameLoop: s.gameTick,
		}, nil
	}

	s.mu.Lock()
	s.playerInput[carId] = &PlayerInput{
		steering:  input.GetSteering(),
		throttle:  input.GetThrottle(),
		brake:     input.GetBrake(),
		timestamp: input.GetTimestamp(),
	}
	s.mu.Unlock()

	return &pb.InputAck{
		Accepted: true,
		GameLoop: s.gameTick,
	}, nil
}
