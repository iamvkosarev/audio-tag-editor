# Audio Tag Editor

A web-based audio tag editor for managing metadata in audio files.

## Quick Start

### Prerequisites

- Docker (running)
- Docker Compose

### Using Make

```bash
make run
```

This will build and start the application using Docker Compose.

### Using Docker

```bash
docker-compose up --build -d
```

The application will be available at `http://localhost:8080` by default. The port can be modified by setting `HTTP_PORT` in the `.env` file.

## Functionality

- **Loading audio files**: Upload and load multiple audio files for editing
- **Group modification**: Select multiple files to apply tag changes to a group
- **Download**: Download files individually or as a group after editing
- **Editing tags**: Edit metadata tags including title, artist, album, year, track, genre, and cover art
- **Column selection**: Customize which columns are visible in the file list
- **Keyboard navigation**: Navigate between rows using arrow keys
- **Dark and light mode**: Toggle between dark and light themes

## Currently Implemented

- **MP3**: Full support for reading and writing tags (title, artist, album, year, track, genre, cover art)
- **FLAC**: Full support for reading and writing tags (title, artist, album, year, track, genre, cover art)

## Planned Features

- **OGG**: Tag writing support (currently only reading is supported)

