const { Empty, RegisterPlayer } = require('../proto/car_pb.js');
const { CarServiceClient } = require('../proto/car_grpc_web_pb.js');

const statusEl     = document.getElementById('status');
const updatesEl    = document.getElementById('updates');
const errorEl      = document.getElementById('error');
const raceInfoEl   = document.getElementById('race-info');
const carsContainer = document.getElementById('cars-container');
const carInfoList  = document.getElementById('car-info-list');
const trackCanvas  = document.getElementById('track-canvas');

let updateCount = 0;
let carStates   = new Map();   // carId → latest CarState
let trackInfo   = null;
let scale       = 1;
let offsetX     = 0;
let offsetY     = 0;

const client = new CarServiceClient('http://localhost:8081', null, null);

console.log('Web observer starting...');

/* ---------------------------------------------------
   TRANSFORM HELPERS (world → screen pixels)
--------------------------------------------------- */
function computeTransform() {
    if (!trackInfo) return;

    const left  = trackInfo.getLeftBoundaryList() || [];
    const right = trackInfo.getRightBoundaryList() || [];
    if (!left.length || !right.length) return;

    let minX = Infinity, maxX = -Infinity;
    let minY = Infinity, maxY = -Infinity;

    [...left, ...right].forEach(p => {
        minX = Math.min(minX, p.getX());
        maxX = Math.max(maxX, p.getX());
        minY = Math.min(minY, p.getY());
        maxY = Math.max(maxY, p.getY());
    });

    const padding = 40;
    const w = trackCanvas.width  || 1100;
    const h = trackCanvas.height || 500;

    scale = Math.min(
        (w - padding * 2) / (maxX - minX || 1),
        (h - padding * 2) / (maxY - minY || 1)
    );

    offsetX = padding - minX * scale;
    offsetY = padding - minY * scale;
}

function worldToScreen(x, y) {
    return {
        left: (x * scale + offsetX) + 'px',
        top:  (y * scale + offsetY) + 'px'
    };
}

/* ---------------------------------------------------
   DRAW TRACK ON CANVAS (background)
--------------------------------------------------- */
function drawTrack() {
    if (!trackCanvas || !trackInfo) return;
    const ctx = trackCanvas.getContext('2d');
    ctx.clearRect(0, 0, trackCanvas.width, trackCanvas.height);

    const left  = trackInfo.getLeftBoundaryList() || [];
    const right = trackInfo.getRightBoundaryList() || [];

    if (!left.length || !right.length) return;

    // Asphalt
    ctx.fillStyle = '#444';
    ctx.beginPath();
    left.forEach((p, i) => {
        const { left: lx, top: ly } = worldToScreen(p.getX(), p.getY());
        const x = parseFloat(lx), y = parseFloat(ly);
        i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
    });
    for (let i = right.length - 1; i >= 0; i--) {
        const { left: lx, top: ly } = worldToScreen(right[i].getX(), right[i].getY());
        ctx.lineTo(parseFloat(lx), parseFloat(ly));
    }
    ctx.closePath();
    ctx.fill();

    // Left boundary (red)
    ctx.strokeStyle = '#ff5555';
    ctx.lineWidth = 3;
    ctx.beginPath();
    left.forEach((p, i) => {
        const { left: lx, top: ly } = worldToScreen(p.getX(), p.getY());
        const x = parseFloat(lx), y = parseFloat(ly);
        i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
    });
    ctx.stroke();

    // Right boundary (blue)
    ctx.strokeStyle = '#5555ff';
    ctx.beginPath();
    right.forEach((p, i) => {
        const { left: lx, top: ly } = worldToScreen(p.getX(), p.getY());
        const x = parseFloat(lx), y = parseFloat(ly);
        i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
    });
    ctx.stroke();
}

/* ---------------------------------------------------
   UPDATE / CREATE CAR DOM ELEMENTS
--------------------------------------------------- */
function updateCarsDisplay() {
    carStates.forEach((state, carId) => {
        let el = document.getElementById(`car-${carId}`);

        if (!el) {
            el = document.createElement('div');
            el.id = `car-${carId}`;
            el.className = 'car';
            el.style.backgroundColor = getCarColor(carId);
            el.innerHTML = carId;
            carsContainer.appendChild(el);
        }

        const pos = state.getPosition();
        if (!pos) return;

        const { left, top } = worldToScreen(pos.getX(), pos.getY());
        const heading = state.getHeading() || 0;

        el.style.left = left;
        el.style.top  = top;
        el.style.transform = `rotate(${heading}deg)`;
        el.title = `Speed: ${state.getSpeed()?.toFixed(1) || '?'} u/s\nLap: ${state.getLap() || '-'}`;
    });

    updateSidebarInfo();
}

/* ---------------------------------------------------
   SIMPLE CAR COLOR BY ID
--------------------------------------------------- */
function getCarColor(carId) {
    const palette = {
        'A': '#e63946', 'B': '#2a9d8f', 'C': '#457b9d',
        'D': '#f4a261', 'E': '#8338ec', 'F': '#ffbe0b',
    };
    return palette[carId] || '#6c757d';
}

/* ---------------------------------------------------
   UPDATE SIDEBAR WITH LATEST CAR INFO
--------------------------------------------------- */
function updateSidebarInfo() {
    carInfoList.innerHTML = '';

    carStates.forEach((state, carId) => {
        const div = document.createElement('div');
        div.className = 'car-info';
        div.style.borderLeft = `6px solid ${getCarColor(carId)}`;

        const pos = state.getPosition() || { getX: () => '?', getY: () => '?' };

        div.innerHTML = `
            <strong>Car ${carId}</strong>
            Position: (${pos.getX()?.toFixed(1) || '?'} , ${pos.getY()?.toFixed(1) || '?'})<br>
            Speed: ${state.getSpeed()?.toFixed(1) || '?'} u/s<br>
            Heading: ${state.getHeading()?.toFixed(0) || '?'}°<br>
            Lap: ${state.getLap() || '-'}
        `;

        carInfoList.appendChild(div);
    });

    if (carStates.size === 0) {
        carInfoList.innerHTML = '<p style="text-align:center;color:#666;">Waiting for cars...</p>';
    }
}

/* ---------------------------------------------------
   CHECK-IN & GET TRACK
--------------------------------------------------- */
function initialize() {
    statusEl.textContent = 'Connecting...';
    statusEl.className = 'disconnected';

    const req = new RegisterPlayer();
    req.setCarId('OBSERVER');
    req.setPlayerName('Web Tracker');
    req.setPassword('spectator');

    client.checkIn(req, {}, (err, res) => {
        if (err || !res.getAccepted()) {
            errorEl.textContent = err?.message || res?.getMessage() || 'Check-in failed';
            statusEl.textContent = 'Failed';
            return;
        }

        statusEl.textContent = 'Connected';
        statusEl.className = 'connected';

        if (res.hasTrack()) {
            trackInfo = res.getTrack();
            computeTransform();
            drawTrack();
        }

        startStream();
    });
}

/* ---------------------------------------------------
   RACE STREAM
--------------------------------------------------- */
function startStream() {
    const stream = client.streamRaceUpdates(new Empty(), {});

    stream.on('data', (raceUpdate) => {
        updateCount++;
        updatesEl.textContent = updateCount;

        const cars = raceUpdate.getCarsList() || [];
        if (cars.length === 0) return;

        // Update latest state for each car
        cars.forEach(car => {
            carStates.set(car.getCarId(), car);
        });

        // Update UI
        updateCarsDisplay();

        // Optional: show race status
        const status = raceUpdate.getRaceStatus();
        if (status) {
            raceInfoEl.textContent = `Status: ${status.getStatus() || '?'} • Laps: ${status.getTotalLaps() || '?'} • Time: ${(status.getRaceTime()/1000).toFixed(1)}s`;
        }
    });

    stream.on('error', err => {
        console.error('Stream error:', err);
        errorEl.textContent = 'Stream error: ' + (err.message || 'unknown');
    });

    stream.on('end', () => {
        console.log('Stream ended');
    });
}

/* ---------------------------------------------------
   START
--------------------------------------------------- */
initialize();