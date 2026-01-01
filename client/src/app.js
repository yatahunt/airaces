const {Empty} = require('../proto/car_pb.js');
const {CarServiceClient} = require('../proto/car_grpc_web_pb.js');

const status = document.getElementById('status');
const updates = document.getElementById('updates');
const errorEl = document.getElementById('error');
const raceInfo = document.getElementById('race-info');
const carsContainer = document.getElementById('cars-container');

let updateCount = 0;
let carInfoMap = {}; // Store static car info by car_id

// Connect to Envoy proxy
const client = new CarServiceClient('http://localhost:8081', null, null);

console.log('Connecting to gRPC server via Envoy proxy at http://localhost:8081');

const request = new Empty();
const stream = client.streamRaceUpdates(request, {});

stream.on('data', (response) => {
    console.log('Received race update:', response.toObject());
    
    // Check if this is a check-in message
    if (response.hasCheckIn()) {
        const checkIn = response.getCheckIn();
        const carsList = checkIn.getCarsList();
        
        console.log('=== CHECK-IN RECEIVED ===');
        console.log(`Total cars: ${carsList.length}`);
        
        // Store static car information
        carsList.forEach(carInfo => {
            const carId = carInfo.getCarId();
            carInfoMap[carId] = {
                carId: carId,
                team: carInfo.getTeam(),
                power: carInfo.getPower(),
                color: carInfo.getColor(),
                driver: carInfo.getDriver()
            };
            
            console.log(`Car ${carId}: Driver=${carInfo.getDriver()}, Team=${carInfo.getTeam()}, Power=${carInfo.getPower()}, Color=${carInfo.getColor()}`);
            
            // Create car element
            createCarElement(carId, carInfo.getColor());
        });
        
        status.textContent = 'Connected - Cars Loaded';
        status.className = 'connected';
    }
    
    // Check if this is race data
    if (response.hasRaceData()) {
        const raceData = response.getRaceData();
        const raceStatus = raceData.getRaceStatus();
        const carsList = raceData.getCarsList();
        
        // Update race status display
        if (raceInfo && raceStatus) {
            const raceTime = (raceStatus.getRaceTime() / 1000).toFixed(1); // Convert to seconds
            raceInfo.innerHTML = `
                Status: ${raceStatus.getStatus().toUpperCase()} | 
                Leader: Car ${raceStatus.getLeaderCarId()} | 
                Time: ${raceTime}s | 
                Laps: ${raceStatus.getTotalLaps()}
            `;
        }
        
        // Update all car positions
        carsList.forEach(carState => {
            const carId = carState.getCarId();
            const x = carState.getX();
            const y = carState.getY();
            const speed = carState.getSpeed();
            const lap = carState.getLap();
            const heading = carState.getHeading();
            
            // Update car element position
            const carElement = document.getElementById(`car-${carId}`);
            if (carElement) {
                carElement.style.left = x + 'px';
                carElement.style.top = y + 'px';
                carElement.style.transform = `rotate(${heading}deg)`;
            }
            
            // Update car info display
            const carInfoElement = document.getElementById(`info-${carId}`);
            if (carInfoElement) {
                const staticInfo = carInfoMap[carId];
                carInfoElement.innerHTML = `
                    <strong>Car ${carId}</strong> (${staticInfo?.driver || 'Unknown'})<br>
                    Speed: ${speed.toFixed(1)} | Lap: ${lap}/${raceStatus.getTotalLaps()}
                `;
            }
        });
        
        updateCount++;
        updates.textContent = updateCount;
        
        if (status.textContent === 'Connected - Cars Loaded') {
            status.textContent = 'Racing';
            status.className = 'connected';
        }
    }
});

function createCarElement(carId, color) {
    // Create car visual element
    const carElement = document.createElement('div');
    carElement.id = `car-${carId}`;
    carElement.className = 'car';
    carElement.style.backgroundColor = color;
    carElement.style.position = 'absolute';
    carElement.style.width = '40px';
    carElement.style.height = '20px';
    carElement.style.borderRadius = '5px';
    carElement.textContent = carId;
    carElement.style.textAlign = 'center';
    carElement.style.lineHeight = '20px';
    carElement.style.fontWeight = 'bold';
    
    if (carsContainer) {
        carsContainer.appendChild(carElement);
    } else {
        document.body.appendChild(carElement);
    }
    
    // Create car info display
    const infoElement = document.createElement('div');
    infoElement.id = `info-${carId}`;
    infoElement.className = 'car-info';
    infoElement.style.marginBottom = '10px';
    infoElement.style.padding = '5px';
    infoElement.style.border = `2px solid ${color}`;
    infoElement.style.borderRadius = '5px';
    
    const infoContainer = document.getElementById('car-info-list');
    if (infoContainer) {
        infoContainer.appendChild(infoElement);
    }
}

stream.on('status', (statusObj) => {
    console.log('Stream status:', statusObj);
});

stream.on('end', () => {
    status.textContent = 'Disconnected';
    status.className = 'disconnected';
    errorEl.textContent = 'Stream ended';
    console.log('Stream ended');
});

stream.on('error', (err) => {
    status.textContent = 'Error';
    status.className = 'disconnected';
    errorEl.textContent = 'Error: ' + err.message;
    console.error('Stream error:', err);
    
    // Retry after 3 seconds
    setTimeout(() => {
        console.log('Retrying connection...');
        location.reload();
    }, 3000);
});