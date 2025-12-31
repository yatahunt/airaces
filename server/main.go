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
	moveInterval = 2 * time.Second
	moveDistance = 50
	trackWidth   = 800
	carStartX    = 0
	carY         = 250
)

type CarServer struct {
	pb.UnimplementedCarServiceServer
	mu       sync.RWMutex
	position *pb.CarPosition
	clients  map[chan *pb.CarPosition]struct{}
}

func NewCarServer() *CarServer {
	s := &CarServer{
		position: &pb.CarPosition{
			X:         carStartX,
			Y:         carY,
			Timestamp: time.Now().Unix(),
		},
		clients: make(map[chan *pb.CarPosition]struct{}),
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

		// Move car forward
		s.position.X += moveDistance

		// Reset to start if reached end of track
		if s.position.X > trackWidth {
			s.position.X = carStartX
			log.Println("Car completed lap, resetting to start")
		}

		s.position.Timestamp = time.Now().Unix()

		// Create a copy of the position to send
		currentPos := &pb.CarPosition{
			X:         s.position.X,
			Y:         s.position.Y,
			Timestamp: s.position.Timestamp,
		}

		log.Printf("Car moved to position X=%d, Y=%d (clients: %d)",
			currentPos.X, currentPos.Y, len(s.clients))

		// Broadcast to all connected clients
		for clientChan := range s.clients {
			select {
			case clientChan <- currentPos:
				// Successfully sent
			default:
				// Client channel full, skip this update
				log.Println("Warning: Client channel full, skipping update")
			}
		}

		s.mu.Unlock()
	}
}

func (s *CarServer) StreamCarPosition(req *pb.Empty, stream pb.CarService_StreamCarPositionServer) error {
	clientChan := make(chan *pb.CarPosition, 10)

	// Register new client
	s.mu.Lock()
	s.clients[clientChan] = struct{}{}
	clientCount := len(s.clients)

	// Send current position immediately to new client
	initialPos := &pb.CarPosition{
		X:         s.position.X,
		Y:         s.position.Y,
		Timestamp: s.position.Timestamp,
	}
	s.mu.Unlock()

	log.Printf("New client connected (total clients: %d)", clientCount)

	// Send initial position
	if err := stream.Send(initialPos); err != nil {
		s.removeClient(clientChan)
		return err
	}

	// Cleanup on disconnect
	defer func() {
		s.removeClient(clientChan)
		log.Printf("Client disconnected (remaining clients: %d)", len(s.clients))
	}()

	// Stream position updates to client
	for pos := range clientChan {
		if err := stream.Send(pos); err != nil {
			log.Printf("Error sending to client: %v", err)
			return err
		}
	}

	return nil
}

func (s *CarServer) removeClient(clientChan chan *pb.CarPosition) {
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
	log.Printf("   - Move interval: %v", moveInterval)
	log.Printf("   - Move distance: %d px", moveDistance)
	log.Printf("   - Track width: %d px", trackWidth)
	log.Printf("   - Starting position: X=%d, Y=%d", carStartX, carY)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
