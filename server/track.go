package main

import (
	"context"
	"encoding/csv"
	"log"
	"math"
	"os"
	pb "server/proto"
	"sort"
	"strconv"
)

func loadTrackFromCSV(filename string) (*pb.TrackInfo, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comment = '#'

	// Read all records
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	// Parse track points
	trackPoints := make([]TrackPoint, 0, len(records))
	for _, record := range records {
		if len(record) < 4 {
			continue
		}

		centerX, _ := strconv.ParseFloat(record[0], 32)
		centerY, _ := strconv.ParseFloat(record[1], 32)
		widthRight, _ := strconv.ParseFloat(record[2], 32)
		widthLeft, _ := strconv.ParseFloat(record[3], 32)

		trackPoints = append(trackPoints, TrackPoint{
			centerX:    float32(centerX),
			centerY:    float32(centerY),
			widthLeft:  float32(widthLeft),
			widthRight: float32(widthRight),
		})
	}

	if len(trackPoints) == 0 {
		log.Fatal("No track points loaded")
	}

	// Generate left and right boundaries
	leftBoundary := make([]*pb.Point3D, len(trackPoints))
	rightBoundary := make([]*pb.Point3D, len(trackPoints))

	for i := 0; i < len(trackPoints); i++ {
		point := trackPoints[i]

		// Calculate tangent vector (direction of track)
		var dx, dy float32
		if i == 0 {
			// Use direction to next point
			dx = trackPoints[i+1].centerX - point.centerX
			dy = trackPoints[i+1].centerY - point.centerY
		} else if i == len(trackPoints)-1 {
			// Use direction from previous point
			dx = point.centerX - trackPoints[i-1].centerX
			dy = point.centerY - trackPoints[i-1].centerY
		} else {
			// Average direction
			dx = trackPoints[i+1].centerX - trackPoints[i-1].centerX
			dy = trackPoints[i+1].centerY - trackPoints[i-1].centerY
		}

		// Normalize tangent
		length := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		if length > 0 {
			dx /= length
			dy /= length
		}

		// Perpendicular vector (rotated 90 degrees)
		// Left is (-dy, dx), Right is (dy, -dx)
		perpX := -dy
		perpY := dx

		// Calculate boundary points
		leftBoundary[i] = &pb.Point3D{
			X: point.centerX + perpX*point.widthLeft,
			Y: point.centerY + perpY*point.widthLeft,
			Z: 0,
		}

		rightBoundary[i] = &pb.Point3D{
			X: point.centerX - perpX*point.widthRight,
			Y: point.centerY - perpY*point.widthRight,
			Z: 0,
		}
	}

	return &pb.TrackInfo{
		TrackId:       "hockenheim",
		Name:          "Hockenheim Circuit",
		LeftBoundary:  leftBoundary,
		RightBoundary: rightBoundary,
	}, nil
}

// GetTrack RPC - returns track information without authentication
func (s *CarServer) GetTrack(ctx context.Context, req *pb.Empty) (*pb.TrackInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.track, nil
}

// Calculate progress along track (0 to 1)
func (s *CarServer) calculateTrackProgress(pos *pb.Point3D) float32 {
	// Simple implementation: find closest track point
	minDist := float32(math.MaxFloat32)
	closestIdx := 0

	for i, leftPoint := range s.track.LeftBoundary {
		rightPoint := s.track.RightBoundary[i]
		centerX := (leftPoint.X + rightPoint.X) / 2
		centerY := (leftPoint.Y + rightPoint.Y) / 2

		dx := pos.X - centerX
		dy := pos.Y - centerY
		dist := dx*dx + dy*dy

		if dist < minDist {
			minDist = dist
			closestIdx = i
		}
	}

	return float32(closestIdx) / float32(len(s.track.LeftBoundary))
}

// Calculate intervals between cars
func (s *CarServer) calculateIntervals() ([]*pb.CarInterval, []*pb.CarInterval) {
	// Sort cars by position (lap then progress)
	type carPosition struct {
		carId    string
		lap      int32
		progress float32
	}

	positions := make([]carPosition, 0, len(s.carStates))
	for carId, state := range s.carStates {
		progress := s.calculateTrackProgress(state.Position)
		positions = append(positions, carPosition{
			carId:    carId,
			lap:      state.Lap,
			progress: progress,
		})
	}

	// Sort by lap (descending) then progress (descending)
	sort.Slice(positions, func(i, j int) bool {
		if positions[i].lap != positions[j].lap {
			return positions[i].lap > positions[j].lap
		}
		return positions[i].progress > positions[j].progress
	})

	// Calculate intervals to leader
	toLeader := make([]*pb.CarInterval, 0, len(positions))
	forPosition := make([]*pb.CarInterval, 0, len(positions))

	if len(positions) == 0 {
		return toLeader, forPosition
	}

	leaderLap := positions[0].lap
	leaderProgress := positions[0].progress

	for i, pos := range positions {
		// Interval to leader (in laps + progress)
		lapDiff := float32(leaderLap - pos.lap)
		progressDiff := leaderProgress - pos.progress
		intervalToLeader := lapDiff + progressDiff

		toLeader = append(toLeader, &pb.CarInterval{
			CarId:    pos.carId,
			Position: int32(i + 1),
			Laps:     pos.lap,
			Interval: intervalToLeader,
		})

		// Interval to car ahead
		intervalForPosition := float32(0.0)
		if i > 0 {
			prevLap := positions[i-1].lap
			prevProgress := positions[i-1].progress
			lapDiff := float32(prevLap - pos.lap)
			progressDiff := prevProgress - pos.progress
			intervalForPosition = lapDiff + progressDiff
		}

		forPosition = append(forPosition, &pb.CarInterval{
			CarId:    pos.carId,
			Position: int32(i + 1),
			Laps:     pos.lap,
			Interval: intervalForPosition,
		})
	}

	return toLeader, forPosition
}
