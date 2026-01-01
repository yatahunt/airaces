const { Empty, PlayerInput, InputAck } = require('../proto/car_pb.js');
const { CarServiceClient } = require('../proto/car_grpc_web_pb.js');

const status = document.getElementById('status');
const updates = document.getElementById('updates');
const errorEl = document.getElementById('error');
const raceInfo = document.getElementById('race-info');
const carsContainer = document.getElementById('cars-container');

let updateCount = 0;
let carInfoMap = {};

// Player info
let myCarId = null;
let authToken = null;
let inputSequence = 0;

// Input state
let currentInput = {
    steering: 0.0,
    throttle: 0.0,
    brake: 0.0,
    boost: false
};

// Track keys pressed
let keysPressed = {
    ArrowLeft: false,
    ArrowRight: false,
    ArrowUp: false,
    ArrowDown: false,
    Space: false
};

// gRPC client via Envoy
const client = new CarServiceClient('http://localhost:8081', null, null);

console.log('Connecting to gRPC server via Envoy at http://localhost:8081');

// ----- Player setup -----
function setupPlayer() {
    myCarId = prompt('Enter your Car ID (e.g., A, B, C):');
    authToken = prompt('Enter your auth token (or press OK for demo token):') || 'demo-token-' + myCarId;

    if (myCarId) {
        console.log(`Playing as Car ${myCarId}`);
        startInputLoop();
        setupKeyboardControls();
    } else {
        console.log('Spectator mode: no inputs sent');
    }
}

// ----- Send input to server -----
function sendInput() {
    if (!myCarId) return;

    const inputMsg = new PlayerInput();
    inputMsg.setCarId(myCarId);
    inputMsg.setAuthToken(authToken);
    inputMsg.setSteering(currentInput.steering);
    inputMsg.setThrottle(currentInput.throttle);
    inputMsg.setBrake(currentInput.brake);
    inputMsg.setBoost(currentInput.boost);
    inputMsg.setTimestamp(Date.now());
    inputMsg.setSequence(inputSequence++);

    client.sendPlayerInput(inputMsg, {}, (err, resp) => {
        if (err) {
            console.error('Input error:', err.message);
            return;
        }
        // Optionally log ack
        // console.log('Input ack:', resp.toObject());
    });
}

// Send input at 60 FPS
function startInputLoop() {
    setInterval(sendInput, 1000 / 60);
}

// ----- Keyboard controls -----
function setupKeyboardControls() {
    document.addEventListener('keydown', e => {
        if (e.repeat) return;
        if (e.code in keysPressed) {
            e.preventDefault();
            keysPressed[e.code] = true;
            updateInputState();
        }
    });
    document.addEventListener('keyup', e => {
        if (e.code in keysPressed) {
            e.preventDefault();
            keysPressed[e.code] = false;
            updateInputState();
        }
    });

    // Display
    const controlsDiv = document.createElement('div');
    controlsDiv.style.cssText = `
        position: fixed; bottom: 20px; right: 20px;
        background: rgba(0,0,0,0.8); color: white;
        padding: 15px; border-radius: 10px;
        font-size: 14px; z-index: 1000;
    `;
    controlsDiv.innerHTML = `
        <strong>🎮 Playing as ${myCarId}</strong><br>
        ⬅️ Left Arrow: Steer Left<br>
        ➡️ Right Arrow: Steer Right<br>
        ⬆️ Up Arrow: Throttle<br>
        ⬇️ Down Arrow: Brake<br>
        Space: Boost<br><br>
        <div id="input-display">Steering: 0.0<br>Throttle: 0.0<br>Brake: 0.0</div>
    `;
    document.body.appendChild(controlsDiv);
}

// Update input state
function updateInputState() {
    currentInput.steering = keysPressed.ArrowLeft && !keysPressed.ArrowRight ? -1.0 :
                            keysPressed.ArrowRight && !keysPressed.ArrowLeft ? 1.0 : 0.0;
    currentInput.throttle = keysPressed.ArrowUp ? 1.0 : 0.0;
    currentInput.brake = keysPressed.ArrowDown ? 1.0 : 0.0;
    currentInput.boost = keysPressed.Space || false;

    const displayEl = document.getElementById('input-display');
    if (displayEl) {
        displayEl.innerHTML = `
            Steering: ${currentInput.steering.toFixed(1)}<br>
            Throttle: ${currentInput.throttle.toFixed(1)}<br>
            Brake: ${currentInput.brake.toFixed(1)}<br>
            ${currentInput.boost ? '🔥 BOOST!' : ''}
        `;
    }
}

// ----- Race updates -----
const stream = client.streamRaceUpdates(new Empty(), {});

stream.on('data', response => {
    if (response.hasCheckIn()) {
        const checkIn = response.getCheckIn();
        checkIn.getCarsList().forEach(car => {
            const carId = car.getCarId();
            carInfoMap[carId] = {
                carId,
                team: car.getTeam(),
                power: car.getPower(),
                color: car.getColor(),
                driver: car.getDriver()
            };
            createCarElement(carId, car.getColor());
        });
        status.textContent = 'Connected - Cars Loaded';
        if (!myCarId) setupPlayer();
    }

    if (response.hasRaceData()) {
        const data = response.getRaceData();
        const cars = data.getCarsList();
        const raceStatus = data.getRaceStatus();

        if (raceInfo) {
            const raceTime = (raceStatus.getRaceTime() / 1000).toFixed(1);
            raceInfo.innerHTML = `Status: ${raceStatus.getStatus().toUpperCase()} | Leader: Car ${raceStatus.getLeaderCarId()} | Time: ${raceTime}s | Laps: ${raceStatus.getTotalLaps()}`;
        }

        cars.forEach(carState => {
            const carId = carState.getCarId();
            const el = document.getElementById(`car-${carId}`);
            if (el) {
                el.style.left = carState.getX() + 'px';
                el.style.top = carState.getY() + 'px';
                el.style.transform = `rotate(${carState.getHeading()}deg)`;
                if (carId === myCarId) {
                    el.style.border = '3px solid yellow';
                    el.style.boxShadow = '0 0 20px rgba(255,255,0,0.8)';
                }
            }

            const infoEl = document.getElementById(`info-${carId}`);
            if (infoEl) {
                const info = carInfoMap[carId];
                const isPlayer = carId === myCarId ? '👤 YOU' : '';
                infoEl.innerHTML = `<strong>Car ${carId} ${isPlayer}</strong> (${info?.driver || 'Unknown'})<br>
                                    Speed: ${carState.getSpeed().toFixed(1)} | Lap: ${carState.getLap()}/${raceStatus.getTotalLaps()}`;
            }
        });

        updateCount++;
        updates.textContent = updateCount;
    }
});

stream.on('status', s => console.log('Stream status:', s));
stream.on('end', () => {
    status.textContent = 'Disconnected';
    status.className = 'disconnected';
    errorEl.textContent = 'Stream ended';
});
stream.on('error', e => {
    status.textContent = 'Error';
    status.className = 'disconnected';
    errorEl.textContent = 'Error: ' + e.message;
    console.error('Stream error:', e);
    setTimeout(() => location.reload(), 3000);
});

// ----- Create car elements -----
function createCarElement(carId, color) {
    const carEl = document.createElement('div');
    carEl.id = `car-${carId}`;
    carEl.className = 'car';
    carEl.style.position = 'absolute';
    carEl.style.width = '40px';
    carEl.style.height = '20px';
    carEl.style.backgroundColor = color;
    carEl.style.borderRadius = '5px';
    carEl.style.textAlign = 'center';
    carEl.style.lineHeight = '20px';
    carEl.style.fontWeight = 'bold';
    carEl.textContent = carId;
    carsContainer.appendChild(carEl);

    const infoEl = document.createElement('div');
    infoEl.id = `info-${carId}`;
    infoEl.className = 'car-info';
    infoEl.style.border = `2px solid ${color}`;
    infoEl.style.padding = '5px';
    infoEl.style.borderRadius = '5px';
    const infoContainer = document.getElementById('car-info-list');
    if (infoContainer) infoContainer.appendChild(infoEl);
}

async function getLocalIP() {
    return new Promise((resolve, reject) => {
        const rtc = new RTCPeerConnection({ iceServers: [] });
        rtc.createDataChannel(""); // create a bogus data channel
        rtc.createOffer().then(offer => rtc.setLocalDescription(offer));

        rtc.onicecandidate = (event) => {
            if (!event || !event.candidate) return;
            const ipMatch = event.candidate.candidate.match(
                /([0-9]{1,3}(\.[0-9]{1,3}){3})/
            );
            if (ipMatch) {
                resolve(ipMatch[1]);
                rtc.close();
            }
        };
    });
}
