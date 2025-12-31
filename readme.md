racing-car/
├── docker-compose.yml
├── server/
│   ├── Dockerfile
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   └── proto/
│       └── car.proto
└── client/
    ├── Dockerfile
    ├── package.json
    ├── index.html
    ├── nginx.conf
    └── proto/
        └── car.proto (copy from server/proto/)