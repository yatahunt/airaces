const {Empty, CarPosition} = require('../proto/car_pb.js');
const {CarServiceClient} = require('../proto/car_grpc_web_pb.js');

const car = document.getElementById('car');
const status = document.getElementById('status');
const posX = document.getElementById('posX');
const posY = document.getElementById('posY');
const updates = document.getElementById('updates');
const errorEl = document.getElementById('error');
let updateCount = 0;

// Connect to Envoy proxy
const client = new CarServiceClient('http://localhost:8081', null, null);

console.log('Connecting to gRPC server via Envoy proxy at http://localhost:8081');

const request = new Empty();
const stream = client.streamCarPosition(request, {});

stream.on('data', (response) => {
    console.log('Received position:', response.toObject());
    
    const x = response.getX();
    const y = response.getY();
    
    car.style.left = x + 'px';
    car.style.top = (y - 200) + 'px';
    
    posX.textContent = x;
    posY.textContent = y;
    updateCount++;
    updates.textContent = updateCount;
    
    if (status.textContent !== 'Connected') {
        status.textContent = 'Connected';
        status.className = 'connected';
        errorEl.textContent = '';
    }
});

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