# Chowkidar

Distributed monitoring system with agent-server architecture.

## Architecture

```
┌─────────────┐  HTTP + Bearer Token  ┌─────────────┐
│   Agent     │ ──────────────────────→ │   Server     │
│  (Go)      │                        │   (Go)      │
│             │                        │              │
│ - Docker    │                        │ - SQLite      │
│ - System    │                        │ - JWT Auth    │
└─────────────┘                        └─────────────┘
                                              ↓
                                        ┌─────────────┐
                                        │ Dashboard   │
                                        │ (Vite+React)│
                                        └─────────────┘
```

## Quick Start

**Development Mode** (Frontend + Backend separate):

Terminal 1 — Backend:
```bash
cd server
./chowkidar
```

Terminal 2 — Frontend:
```bash
cd frontend
npm install
npm run dev
```

Then open `http://localhost:5173` for the frontend.

**API Only**:
```bash
cd server
./chowkidar
```

API runs on `http://localhost:8080`.

## Project Structure

```
chowkidar/
├── agent/              # Agent code (future)
├── server/
│   ├── api/            # HTTP handlers, JWT
│   ├── config/          # Environment config
│   ├── store/           # SQLite storage
│   └── main.go          # Entry point
├── frontend/           # Vite + React
│   ├── src/
│   │   ├── api/client.ts
│   │   ├── pages/Login.tsx
│   │   └── pages/Dashboard.tsx
│   └── vite.config.ts
├── docs/
│   └── agent-teams-reference.md
└── .env.example
```

## Environment Variables

Copy `.env.example` to `.env` and configure:

```bash
ADMIN_USERNAME=admin      # Default admin username
ADMIN_PASSWORD=admin      # Default admin password
SERVER_PORT=8080          # Server port
JWT_SECRET=...              # JWT signing secret
DB_PATH=./chowkidar.db # SQLite database path
```

## API Endpoints

```
POST   /api/v1/auth/login   # Login → JWT token
GET    /api/v1/auth/me      # Verify token → username
```

## License

MIT
