# Overview

Simple tool to serve files from a given directory.

Supports watching for files changes and emitting an event over a websocket.

# Example

```sh
# Serve the current directory at / and watch for html, js, or css file changes.
http-watch -dir=. -pattern=".*(.html|.js|.css)"
```
