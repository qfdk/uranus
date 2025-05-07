# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Uranus (Οὐρανός) is a web-based Nginx management UI written in Go using the Gin framework. It provides a graphical interface for managing Nginx configurations, including adding, deleting, updating, and querying all Nginx settings. The application includes features such as:

- SSL certificate auto-renewal using Lego
- Smooth updates with tableflip
- Web terminal integration
- Database support using GORM with SQLite
- One-click upgrade functionality

## Development Environment Setup

### Prerequisites

- Go 1.23+ with Go 1.24+ toolchain
- Node.js with pnpm for CSS building
- Nginx (for testing)

## Build Commands

### Go Application Build

```bash
# Build the application
make build

# Clean the build artifacts
make clean

# Build for release (creates linux amd64 and arm64 binaries)
make release

# Build for arm64 requires aarch64-linux-gnu-gcc
# Install with: apt-get install -y gcc-aarch64-linux-gnu
```

### Frontend Assets

```bash
# Build CSS assets (TailwindCSS)
make css
# or
pnpm run build:css

# Watch CSS files for changes during development
pnpm run dev:css
```

## System Architecture

### Core Components

1. **Web Server (Gin)**: Handles HTTP requests, serves static files, and renders HTML templates
2. **MQTT Terminal**: Provides terminal access through browser 
3. **SSL Management**: Automatic certificate renewal using Lego (Let's Encrypt)
4. **Nginx Integration**: Management of Nginx configurations
5. **SQLite Database**: Storage for application data

### Main Application Flow

1. The application initializes with `Graceful()` which sets up tableflip for zero-downtime upgrades
2. Configuration is loaded from the application config
3. Database is initialized
4. MQTT terminal service is started
5. SSL renewal service is started
6. Agent heartbeat service is started
7. HTTP server is initialized with all routes

### Key Files and Directories

- `main.go`: Application entry point
- `internal/config/`: Application configuration
- `internal/controllers/`: HTTP request handlers
- `internal/middlewares/`: HTTP middleware components
- `internal/models/`: Database models
- `internal/mqtty/`: MQTT terminal functionality
- `internal/routes/`: HTTP route definitions
- `internal/services/`: Business logic services
- `internal/tools/`: Utility functions
- `web/`: HTML templates and static assets

## Deployment

The application is designed to be installed as a systemd service using the provided installation script:

```bash
# Install (only tested on Ubuntu 20.04/22.04)
wget -qO- https://fr.qfdk.me/uranus/install.sh|bash

# Upgrade
wget -qO- https://fr.qfdk.me/uranus/upgrade.sh|bash

# Kill the process (if needed)
kill -9 $(ps aux|grep "uranus"|grep -v grep|awk '{print $2}')
```

## Running in Development Mode

By default, the application runs in release mode with GIN_MODE=release. For development:

```bash
# Run in development mode
go run .

# Run with specific port (default is 7777)
go run . --port 8080

# Check version
go run . --version
```