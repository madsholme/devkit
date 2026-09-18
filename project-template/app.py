"""A small acceptance-test app: edit message.txt and test isolated persisted data."""
from html import escape
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import os
from pathlib import Path
import sqlite3

data_dir = Path(os.environ.get("DATA_DIR", ".data"))
data_dir.mkdir(parents=True, exist_ok=True)
database = data_dir / "preview.sqlite"
with sqlite3.connect(database) as db:
    db.execute("CREATE TABLE IF NOT EXISTS counter (id INTEGER PRIMARY KEY, value INTEGER)")
    db.execute("INSERT OR IGNORE INTO counter VALUES (1, 0)")


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")
            return
        with sqlite3.connect(database) as db:
            count = db.execute("SELECT value FROM counter WHERE id = 1").fetchone()[0]
        message = escape(Path("message.txt").read_text().strip())
        mode = escape(os.environ.get("DEVBOX_MODE", "unknown"))
        commit = escape(os.environ.get("DEVBOX_COMMIT", "working-files"))
        body = f"""<!doctype html><html lang="en"><meta charset="utf-8">
        <meta name="viewport" content="width=device-width,initial-scale=1">
        <title>Devbox preview</title><style>
        body{{font:18px system-ui;max-width:760px;margin:10vh auto;padding:24px;color:#18332e;background:#f3f6f1}}
        code{{overflow-wrap:anywhere}}button{{font:inherit;padding:10px 18px;cursor:pointer}}
        </style><h1>{message}</h1><p>Mode: <strong>{mode}</strong></p>
        <p>Commit: <code>{commit}</code></p><p>This preview's counter: <strong>{count}</strong></p>
        <form method="post" action="/increment"><button>Increment test data</button></form>
        <p>Edit message.txt to check a live preview. Other previews should keep their own message and counter.</p>
        </html>""".encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        if self.path != "/increment":
            self.send_error(404)
            return
        with sqlite3.connect(database) as db:
            db.execute("UPDATE counter SET value = value + 1 WHERE id = 1")
        self.send_response(303)
        self.send_header("Location", "/")
        self.end_headers()


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
