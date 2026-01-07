const { Empty, RegisterPlayer } = require('../proto/car_pb.js');
const { CarServiceClient } = require('../proto/car_grpc_web_pb.js');

const statusEl     = document.getElementById('status');
const updatesEl    = document.getElementById('updates');
const errorEl      = document.getElementById('error');
const raceInfoEl   = document.getElementById('race-info');
const carsContainer = document.getElementById('cars-container');
const carInfoList  = document.getElementById('car-info-list');
const trackCanvas  = document.getElementById('track-canvas');
const penaltiesEl  = document.getElementById('penalties');

let updateCount = 0;
let carStates   = new Map();
let carPenalties = new Map();
let trackInfo   = null;
let scale       = 1;
let offsetX     = 0;
let offsetY     = 0;

const client = new CarServiceClient('http://localhost:8081', null, null);

console.log('=== WEB OBSERVER STARTING ===');
console.log('Client created:', client);

/* ---------------------------------------------------
   CAR STATUS ENUM NAMES
--------------------------------------------------- */
const CarStatusNames = {
    0: 'NOT READY',
    1: 'WAITING',
    2: 'RACING',
    3: 'SERVING PENALTY',
    99: 'FINISHED'
};

const RaceTypeNames = {
    0: 'HOTLAP',
    1: 'QUALIFYING',
    2: 'RACE BY LAPS',
    3: 'RACE BY TIME'
};

/* ---------------------------------------------------
   TRANSFORM HELPERS
--------------------------------------------------- */
function computeTransform() {
    console.log('[computeTransform] Starting...');
    if (!trackInfo) {
        console.log('[computeTransform] No trackInfo available');
        return;
    }

    const left  = trackInfo.getLeftBoundaryList() || [];
    const right = trackInfo.getRightBoundaryList() || [];
    console.log(`[computeTransform] Boundaries: left=${left.length}, right=${right.length}`);
    
    if (!left.length || !right.length) {
        console.log('[computeTransform] No boundary points');
        return;
    }

    let minX = Infinity, maxX = -Infinity;
    let minY = Infinity, maxY = -Infinity;

    [...left, ...right].forEach(p => {
        minX = Math.min(minX, p.getX());
        maxX = Math.max(maxX, p.getX());
        minY = Math.min(minY, p.getY());
        maxY = Math.max(maxY, p.getY());
    });

    console.log(`[computeTransform] Bounds: X=[${minX}, ${maxX}], Y=[${minY}, ${maxY}]`);

    const padding = 40;
    const w = trackCanvas.width  || 1100;
    const h = trackCanvas.height || 500;

    scale = Math.min(
        (w - padding * 2) / (maxX - minX || 1),
        (h - padding * 2) / (maxY - minY || 1)
    );

    offsetX = padding - minX * scale;
    offsetY = padding - minY * scale;

    console.log(`[computeTransform] Transform: scale=${scale.toFixed(2)}, offset=(${offsetX.toFixed(1)}, ${offsetY.toFixed(1)})`);
}

function worldToScreen(x, y) {
    return {
        left: (x * scale + offsetX) + 'px',
        top:  (y * scale + offsetY) + 'px'
    };
}

/* ---------------------------------------------------
   DRAW TRACK ON CANVAS
--------------------------------------------------- */
function drawTrack() {
    console.log('[drawTrack] Starting...');
    if (!trackCanvas) {
        console.error('[drawTrack] No canvas element!');
        return;
    }
    if (!trackInfo) {
        console.log('[drawTrack] No track info yet');
        return;
    }

    const ctx = trackCanvas.getContext('2d');
    console.log('[drawTrack] Canvas context:', ctx ? 'OK' : 'FAILED');
    
    ctx.clearRect(0, 0, trackCanvas.width, trackCanvas.height);

    const left  = trackInfo.getLeftBoundaryList() || [];
    const right = trackInfo.getRightBoundaryList() || [];

    if (!left.length || !right.length) {
        console.log('[drawTrack] No boundaries to draw');
        return;
    }

    console.log(`[drawTrack] Drawing ${left.length} boundary points`);

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

    console.log('[drawTrack] Track drawn successfully');
}

/* ---------------------------------------------------
   UPDATE CAR DISPLAY
--------------------------------------------------- */
function updateCarsDisplay() {
    console.log(`[updateCarsDisplay] Updating ${carStates.size} cars`);
    
    carStates.forEach((state, carId) => {
        console.log(`[updateCarsDisplay] Car ${carId}:`, {
            position: state.getPosition() ? `(${state.getPosition().getX()}, ${state.getPosition().getY()})` : 'null',
            heading: state.getHeading(),
            speed: state.getSpeed(),
            status: state.getStatus()
        });

        let el = document.getElementById(`car-${carId}`);

        if (!el) {
            console.log(`[updateCarsDisplay] Creating new element for car ${carId}`);
            el = document.createElement('div');
            el.id = `car-${carId}`;
            el.className = 'car';
            el.style.backgroundColor = getCarColor(carId);
            el.innerHTML = carId;
            carsContainer.appendChild(el);
        }

        const pos = state.getPosition();
        if (!pos) {
            console.log(`[updateCarsDisplay] Car ${carId} has no position`);
            return;
        }

        const { left, top } = worldToScreen(pos.getX(), pos.getY());
        const heading = state.getHeading() || 0;

        console.log(`[updateCarsDisplay] Car ${carId} screen position: ${left}, ${top}`);

        el.style.left = left;
        el.style.top  = top;
        el.style.transform = `rotate(${heading}deg)`;
        
        // Status-based styling
        const status = state.getStatus();
        if (status === 3) { // SERVINGPENALTY
            el.style.opacity = '0.5';
            el.style.border = '3px solid #ff0000';
        } else if (status === 99) { // FINISHED
            el.style.opacity = '0.7';
            el.style.border = '3px solid #00ff00';
        } else {
            el.style.opacity = '1';
            el.style.border = '2px solid rgba(255,255,255,0.3)';
        }

        const statusName = CarStatusNames[status] || 'UNKNOWN';
        el.title = `Status: ${statusName}\nSpeed: ${state.getSpeed()?.toFixed(1) || '?'} u/s\nLap: ${state.getLap() || '-'}`;
    });

    updateSidebarInfo();
    updatePenaltiesDisplay();
}

function getCarColor(carId) {
    const palette = {
        'A': '#e63946', 'B': '#2a9d8f', 'C': '#457b9d',
        'D': '#f4a261', 'E': '#8338ec', 'F': '#ffbe0b',
    };
    return palette[carId] || '#6c757d';
}

/* ---------------------------------------------------
   UPDATE SIDEBAR
--------------------------------------------------- */
function updateSidebarInfo() {
    console.log('[updateSidebarInfo] Updating sidebar');
    carInfoList.innerHTML = '';

    carStates.forEach((state, carId) => {
        const div = document.createElement('div');
        div.className = 'car-info';
        div.style.borderLeft = `6px solid ${getCarColor(carId)}`;

        const pos = state.getPosition() || { getX: () => '?', getY: () => '?' };
        const status = state.getStatus();
        const statusName = CarStatusNames[status] || 'UNKNOWN';
        const statusClass = status === 3 ? 'penalty-status' : status === 99 ? 'finished-status' : '';

        div.innerHTML = `
            <strong>Car ${carId}</strong>
            <span class="${statusClass}" style="float:right; font-size:0.8em; font-weight:bold;">${statusName}</span><br>
            Position: (${pos.getX()?.toFixed(1) || '?'}, ${pos.getY()?.toFixed(1) || '?'})<br>
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
   UPDATE PENALTIES
--------------------------------------------------- */
function updatePenaltiesDisplay() {
    if (!penaltiesEl) return;

    if (carPenalties.size === 0) {
        penaltiesEl.style.display = 'none';
        return;
    }

    console.log(`[updatePenaltiesDisplay] Showing ${carPenalties.size} penalties`);
    penaltiesEl.style.display = 'block';
    let html = '<h3>⚠️ Active Penalties</h3>';

    carPenalties.forEach((penalty, carId) => {
        const remainingSec = (penalty.getRemainingPenalty() / 1000).toFixed(1);
        const color = getCarColor(carId);
        html += `
            <div class="penalty-item" style="border-left: 4px solid ${color};">
                <strong>Car ${carId}</strong>: ${penalty.getReason()}<br>
                <small>Remaining: ${remainingSec}s</small>
            </div>
        `;
    });

    penaltiesEl.innerHTML = html;
}

/* ---------------------------------------------------
   INITIALIZE - LOAD TRACK
--------------------------------------------------- */
function initialize() {
    console.log('[initialize] Starting initialization...');
    statusEl.textContent = 'Connecting...';
    statusEl.className = 'disconnected';
    errorEl.textContent = '';

    console.log('[initialize] Calling GetTrack...');

    // Load track using GetTrack
    client.getTrack(new Empty(), {}, (err, track) => {
        console.log('[GetTrack] Callback triggered');
        
        if (err) {
            console.error('[GetTrack] ERROR:', err);
            console.error('[GetTrack] Error details:', JSON.stringify(err));
            errorEl.textContent = 'GetTrack failed: ' + (err.message || 'unknown');
            statusEl.textContent = 'Failed';
            return;
        }

        console.log('[GetTrack] SUCCESS');
        console.log('[GetTrack] Track object:', track);
        console.log('[GetTrack] Track name:', track.getName());
        console.log('[GetTrack] Track ID:', track.getTrackId());

        trackInfo = track;
        
        computeTransform();
        drawTrack();

        statusEl.textContent = 'Connected (Observer)';
        statusEl.className = 'connected';

        console.log('[initialize] Starting race stream...');
        startStream();
    });
}

/* ---------------------------------------------------
   RACE STREAM
--------------------------------------------------- */
function startStream() {
    console.log('[startStream] Creating stream...');
    const stream = client.streamRaceUpdates(new Empty(), {});
    console.log('[startStream] Stream object:', stream);

    stream.on('data', (raceUpdate) => {
        updateCount++;
        updatesEl.textContent = updateCount;

        if (updateCount === 1) {
            console.log('[stream:data] ★★★ FIRST RACE UPDATE RECEIVED ★★★');
        }
        if (updateCount % 60 === 0) {
            console.log(`[stream:data] Update #${updateCount}`);
        }

        const cars = raceUpdate.getCarsList() || [];
        console.log(`[stream:data] Received ${cars.length} cars`);
        
        // Update car states
        cars.forEach(car => {
            const carId = car.getCarId();
            console.log(`[stream:data] Car ${carId}: status=${car.getStatus()}, speed=${car.getSpeed()}`);
            carStates.set(carId, car);
        });

        // Update penalties
        carPenalties.clear();
        const penalties = raceUpdate.getPenaltiesList() || [];
        if (penalties.length > 0) {
            console.log(`[stream:data] Received ${penalties.length} penalties`);
        }
        penalties.forEach(penalty => {
            carPenalties.set(penalty.getCarId(), penalty);
        });

        // Update UI
        updateCarsDisplay();

        // Show race status
        const status = raceUpdate.getRaceStatus();
        if (status) {
            const gameTick = status.getGameTick() || 0;
            const statusText = status.getStatus() || '?';
            const totalLaps = status.getTotalLaps() || '?';
            raceInfoEl.textContent = `Status: ${statusText} • Laps: ${totalLaps} • Tick: ${gameTick}`;
        }
    });

    stream.on('error', err => {
        console.error('[stream:error] ERROR:', err);
        console.error('[stream:error] Error details:', JSON.stringify(err));
        errorEl.textContent = 'Stream error: ' + (err.message || 'unknown');
        statusEl.textContent = 'Stream failed';
        statusEl.className = 'disconnected';
    });

    stream.on('end', () => {
        console.log('[stream:end] Stream ended normally');
        statusEl.textContent = 'Disconnected';
        statusEl.className = 'disconnected';
    });

    stream.on('status', status => {
        console.log('[stream:status]', status);
    });

    console.log('[startStream] Stream listeners attached');
}

/* ---------------------------------------------------
   START
--------------------------------------------------- */
console.log('[main] Calling initialize()...');
initialize();
console.log('[main] Initialize() called, waiting for async responses...');