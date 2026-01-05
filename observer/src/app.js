const { Empty, RegisterPlayer } = require('../proto/car_pb.js');
const { CarServiceClient } = require('../proto/car_grpc_web_pb.js');

const status = document.getElementById('status');
const updates = document.getElementById('updates');
const errorEl = document.getElementById('error');
const raceInfo = document.getElementById('race-info');
const carsContainer = document.getElementById('cars-container');
const trackCanvas = document.getElementById('track-canvas');

let updateCount = 0;
let carInfoMap = {};
let trackInfo = null;

const client = new CarServiceClient('http://localhost:8081', null, null);

console.log('Connecting to gRPC server via Envoy proxy');

/* ---------------------------------------------------
   TRACK POINT DEBUG
--------------------------------------------------- */
function printTrackPoints(track) {
    console.log('Track object:', track.toObject());
}

/* ---------------------------------------------------
   INIT
--------------------------------------------------- */
function initialize() {
    status.textContent = 'Checking in...';

    const req = new RegisterPlayer();
    req.setCarId('OBSERVER');
    req.setPlayerName('Web Observer');
    req.setPassword('spectator');

    const checkIn = client.checkIn || client.CheckIn;
    checkIn.call(client, req, {}, (err, res) => {
        if (err || !res.getAccepted()) {
            errorEl.textContent = err?.message || res.getMessage();
            return;
        }

        if (res.hasTrack()) {
            trackInfo = res.getTrack();
            printTrackPoints(trackInfo);
            drawTrackPoints(trackInfo);
        }

        startRaceStream();
        status.textContent = 'Connected';
    });
}

/* ---------------------------------------------------
   DRAW ALL TRACK POINTS
--------------------------------------------------- */
function drawTrackPoints(track) {
    if (!trackCanvas) return;
    const ctx = trackCanvas.getContext('2d');

    const left = track.getLeftBoundaryList();
    const right = track.getRightBoundaryList();
    if (!left.length || !right.length) return;

    // ---------- 1. Compute world bounds ----------
    let minX = Infinity, maxX = -Infinity;
    let minY = Infinity, maxY = -Infinity;

    [...left, ...right].forEach(p => {
        minX = Math.min(minX, p.getX());
        maxX = Math.max(maxX, p.getX());
        minY = Math.min(minY, p.getY());
        maxY = Math.max(maxY, p.getY());
    });

    const worldWidth = maxX - minX;
    const worldHeight = maxY - minY;

    // ---------- 2. Scale & center ----------
    const padding = 40;
    const scale = Math.min(
        (trackCanvas.width - padding * 2) / worldWidth,
        (trackCanvas.height - padding * 2) / worldHeight
    );

    const offsetX = padding - minX * scale;
    const offsetY = padding - minY * scale;

    const toCanvas = (p) => ({
        x: p.getX() * scale + offsetX,
        y: p.getY() * scale + offsetY
    });

    

    // ---------- 4. Draw circuit surface ----------
    ctx.fillStyle = '#777'; // asphalt grey
    ctx.beginPath();

    // Left boundary (forward)
    left.forEach((p, i) => {
        const { x, y } = toCanvas(p);
        i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
    });

    // Right boundary (reverse)
    for (let i = right.length - 1; i >= 0; i--) {
        const { x, y } = toCanvas(right[i]);
        ctx.lineTo(x, y);
    }

    ctx.closePath();
    ctx.fill();

    // ---------- 5. Optional: boundary outlines ----------
    ctx.lineWidth = 1;

    ctx.strokeStyle = '#ff4444'; // left
    ctx.beginPath();
    left.forEach((p, i) => {
        const { x, y } = toCanvas(p);
        i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
    });
    ctx.stroke();

    ctx.strokeStyle = '#4488ff'; // right
    ctx.beginPath();
    right.forEach((p, i) => {
        const { x, y } = toCanvas(p);
        i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
    });
    ctx.stroke();

    console.log(
        `Track rendered: left=${left.length}, right=${right.length}`
    );
}

/* ---------------------------------------------------
   STREAM
--------------------------------------------------- */
function startRaceStream() {
    const req = new Empty();
    const stream = client.streamRaceUpdates.call(client, req, {});

    stream.on('data', () => {
        updates.textContent = ++updateCount;
    });
}

/* ---------------------------------------------------
   START
--------------------------------------------------- */
initialize();
