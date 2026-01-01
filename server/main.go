package main

import (
	"log"
	"net"
	"sync"
	"time"

	pb "server/proto" // local import

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	port         = ":50051"
	moveInterval = time.Second / 15
	moveDistance = 50.0 / 15.0
	trackWidth   = 500.0
	carStartX    = 450.0
	carY         = 250.0
	numCars      = 5
	totalLaps    = 3
)

type CarInfo struct {
	carId  string
	team   string
	power  float32
	color  string
	driver string
	x      float32
	y      float32
}

type CarServer struct {
	pb.UnimplementedCarServiceServer
	mu          sync.RWMutex
	carInfos    []CarInfo
	carStates   map[string]*pb.CarState
	raceStatus  *pb.RaceStatus
	raceStarted time.Time
	clients     map[chan *pb.RaceUpdate]struct{}
}

func NewCarServer() *CarServer {
	// Initialize static car information
	carInfos := make([]CarInfo, numCars)
	carStates := make(map[string]*pb.CarState)

	colors := []string{"#FF0000", "#00FF00", "#0000FF", "#FFFF00", "#FF00FF"}
	drivers := []string{"Alice", "Bob", "Charlie", "Diana", "Eve"}

	for i := 0; i < numCars; i++ {
		carId := string(rune('A' + i))
		carInfos[i] = CarInfo{
			carId:  carId,
			team:   "Team " + string(rune('1'+i)),
			power:  float32(80 + i*5),
			color:  colors[i],
			driver: drivers[i],
			x:      carStartX + float32(i*50),
			y:      carY + float32(i*60),
		}

		carStates[carId] = &pb.CarState{
			CarId:     carId,
			X:         carInfos[i].x,
			Y:         carInfos[i].y,
			Heading:   0.0,
			Speed:     0.0,
			Lap:       0,
			Timestamp: time.Now().UnixNano(),
		}
	}

	s := &CarServer{
		carInfos:  carInfos,
		carStates: carStates,
		raceStatus: &pb.RaceStatus{
			Status:      "racing",
			TotalLaps:   totalLaps,
			RaceTime:    0,
			LeaderCarId: "A",
		},
		raceStarted: time.Now(),
		clients:     make(map[chan *pb.RaceUpdate]struct{}),
	}

	go s.startCarMovement()

	return s
}

func (s *CarServer) startCarMovement() {
	ticker := time.NewTicker(moveInterval)
	defer ticker.Stop()

	log.Println("Car movement started - moving every", moveInterval)

	for range ticker.C {
		s.mu.Lock()

		var leader string
		maxLap := int32(0)
		maxX := float32(0)

		// Move each car forward with slightly different speeds
		for i, carInfo := range s.carInfos {
			carState := s.carStates[carInfo.carId]

			speedMultiplier := 1.0 + float32(i)*0.1
			carState.X -= moveDistance * speedMultiplier
			carState.Speed = moveDistance * speedMultiplier * 15

			// Reset to start if reached end of track
			if carState.X < 0 {
				carState.X = carStartX
				carState.Lap++
				log.Printf("Car %s completed lap %d", carState.CarId, carState.Lap)
			}

			carState.Timestamp = time.Now().UnixNano()

			// Determine leader
			if carState.Lap > maxLap || (carState.Lap == maxLap && carState.X > maxX) {
				maxLap = carState.Lap
				maxX = carState.X
				leader = carState.CarId
			}
		}

		// Update race status
		s.raceStatus.RaceTime = time.Since(s.raceStarted).Milliseconds()
		s.raceStatus.LeaderCarId = leader

		// Check if race is finished
		if maxLap >= totalLaps {
			s.raceStatus.Status = "finished"
		}

		// Create race update
		raceUpdate := s.createRaceUpdate()

		log.Printf("Race update - Leader: %s, Time: %dms, Clients: %d",
			leader, s.raceStatus.RaceTime, len(s.clients))

		// Broadcast to all connected clients
		for clientChan := range s.clients {
			select {
			case clientChan <- raceUpdate:
				// Successfully sent
			default:
				// Client channel full, skip this update
				log.Println("Warning: Client channel full, skipping update")
			}
		}

		s.mu.Unlock()
	}
}

func (s *CarServer) createRaceUpdate() *pb.RaceUpdate {
	carStates := make([]*pb.CarState, 0, len(s.carStates))
	for _, state := range s.carStates {
		carStates = append(carStates, &pb.CarState{
			CarId:     state.CarId,
			X:         state.X,
			Y:         state.Y,
			Heading:   state.Heading,
			Speed:     state.Speed,
			Lap:       state.Lap,
			Timestamp: state.Timestamp,
		})
	}

	return &pb.RaceUpdate{
		Update: &pb.RaceUpdate_RaceData{
			RaceData: &pb.RaceData{
				RaceStatus: &pb.RaceStatus{
					Status:      s.raceStatus.Status,
					TotalLaps:   s.raceStatus.TotalLaps,
					RaceTime:    s.raceStatus.RaceTime,
					LeaderCarId: s.raceStatus.LeaderCarId,
				},
				Cars:      carStates,
				Timestamp: time.Now().UnixNano(),
			},
		},
	}
}

func (s *CarServer) StreamRaceUpdates(req *pb.Empty, stream pb.CarService_StreamRaceUpdatesServer) error {
	clientChan := make(chan *pb.RaceUpdate, 10)

	// Register new client
	s.mu.Lock()
	s.clients[clientChan] = struct{}{}
	clientCount := len(s.clients)
	s.mu.Unlock()

	log.Printf("New client connected (total clients: %d)", clientCount)

	// Send check-in message with static car information
	carInfos := make([]*pb.CarInfo, len(s.carInfos))
	for i, info := range s.carInfos {
		carInfos[i] = &pb.CarInfo{
			CarId:  info.carId,
			Team:   info.team,
			Power:  info.power,
			Color:  info.color,
			Driver: info.driver,
		}
	}

	checkInUpdate := &pb.RaceUpdate{
		Update: &pb.RaceUpdate_CheckIn{
			CheckIn: &pb.CheckIn{
				Cars:      carInfos,
				Timestamp: time.Now().UnixNano(),
			},
		},
	}

	if err := stream.Send(checkInUpdate); err != nil {
		s.removeClient(clientChan)
		return err
	}

	log.Printf("Sent check-in data to client with %d cars", len(carInfos))

	// Send initial race data
	s.mu.Lock()
	initialRaceUpdate := s.createRaceUpdate()
	s.mu.Unlock()

	if err := stream.Send(initialRaceUpdate); err != nil {
		s.removeClient(clientChan)
		return err
	}

	// Cleanup on disconnect
	defer func() {
		s.removeClient(clientChan)
		log.Printf("Client disconnected (remaining clients: %d)", len(s.clients))
	}()

	// Stream race updates to client
	for raceUpdate := range clientChan {
		if err := stream.Send(raceUpdate); err != nil {
			log.Printf("Error sending to client: %v", err)
			return err
		}
	}

	return nil
}

func (s *CarServer) removeClient(clientChan chan *pb.RaceUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.clients, clientChan)
	close(clientChan)
}

func main() {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", port, err)
	}

	grpcServer := grpc.NewServer()
	carServer := NewCarServer()

	pb.RegisterCarServiceServer(grpcServer, carServer)

	// Enable reflection for debugging with tools like grpcurl
	reflection.Register(grpcServer)

	log.Printf("🏎️  gRPC Racing Car Server listening on %s", port)
	log.Printf("📊 Configuration:")
	log.Printf("   - Number of cars: %d", numCars)
	log.Printf("   - Total laps: %d", totalLaps)
	log.Printf("   - Move interval: %v", moveInterval)
	log.Printf("   - Move distance: %.2f px", moveDistance)
	log.Printf("   - Track width: %.2f px", trackWidth)
	log.Printf("   - Starting position: X=%.2f, Y=%.2f", carStartX, carY)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
