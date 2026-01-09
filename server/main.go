package main

import (
	"log"
	"net"
	"sync"
	"time"

	pb "server/proto" // your generated proto package

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	port             = ":50051"
	updateRate       = time.Second / 60 // 60 FPS
	numCars          = 5
	totalLaps        = 3
	raceTime         = 600 // 10 minutes for time-based races
	observersallowed = true
	observersID      = "OBSERVER"
	observerstoken   = "OBSERVERTOKEN"
	maxSpeed         = float32(300.0)
	acceleration     = float32(200.0)
	brakeForce       = float32(400.0)
	friction         = float32(50.0)
	turnSpeed        = float32(180.0)
)

type TrackPoint struct {
	centerX    float32
	centerY    float32
	widthLeft  float32
	widthRight float32
}

type PlayerInput struct {
	steering  float32
	throttle  float32
	brake     float32
	timestamp int32
}

type CarInfo struct {
	carId  string
	power  float32
	weight float32
	x      float32
	y      float32
	z      float32
}

// Extended car state for lap detection
type CarStateExtended struct {
	*pb.CarState
	lastProgress    float32
	crossedFinish   bool
	bestLapTime     float32
	currentLapStart time.Time
	lapTimes        []float32
}

type CarServer struct {
	pb.UnimplementedCarServiceServer
	mu           sync.RWMutex
	carInfos     []CarInfo
	carStates    map[string]*CarStateExtended
	playerInput  map[string]*PlayerInput
	authTokens   map[string]string
	penalties    map[string]*pb.CarPenalty
	raceStatus   *pb.RaceStatus
	raceStarted  time.Time
	gameTick     int32
	clients      map[chan *pb.RaceUpdate]struct{}
	track        *pb.TrackInfo
	raceType     pb.RaceType
	raceLaps     int32
	raceTimeLeft int32 // seconds remaining for time-based races
}

func main() {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	carServer := NewCarServer()

	pb.RegisterCarServiceServer(grpcServer, carServer)
	reflection.Register(grpcServer)

	log.Printf("🏎️  Racing server listening on %s", port)
	log.Printf("Race type: %v", carServer.raceType)
	if carServer.raceType == pb.RaceType_RACEBYLAPS {
		log.Printf("Total laps: %d", carServer.raceLaps)
	} else if carServer.raceType == pb.RaceType_RACEBYTIME {
		log.Printf("Race duration: %d seconds", raceTime)
	}

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
