"""
LANHub Agent — runs on every machine you want to browse from the hub.

Implements the contract in docs/AGENT_API.md. Works identically on Windows
and Linux; the only OS-specific bits are drive enumeration and hidden-file
detection.
"""
from __future__ import annotations

import json
import logging
import mimetypes
import os
import platform
import shutil
import sys
import threading
import time
import uuid
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from datetime import datetime, timezone
from logging.handlers import RotatingFileHandler
from pathlib import Path

from fastapi import Depends, FastAPI, HTTPException, Header, Query, Request, UploadFile
from fastapi.responses import JSONResponse, PlainTextResponse, StreamingResponse
from pydantic import BaseModel

CONFIG_PATH = Path(os.environ.get("LANHUB_AGENT_CONFIG", Path(__file__).parent / "config.json"))

if not CONFIG_PATH.exists():
    sys.exit(
        f"Missing config file at {CONFIG_PATH}. Copy config.example.json to "
        f"config.json and set a token before starting the agent."
    )

CONFIG = json.loads(CONFIG_PATH.read_text())
TOKEN = CONFIG["token"]
ALLOWED_ROOTS = [Path(r).resolve() for r in CONFIG.get("allowed_roots", [])]
THROTTLE_KBPS = CONFIG.get("throttle_kbps", 0) or 0
CHUNK_SIZE = 256 * 1024

# --- Logging -----------------------------------------------------------
# Every request (except the once-a-minute health poll, which would drown
# out everything else) is logged as one JSON line. This doubles as the
# activity/audit trail the hub reads via GET /api/activity — there's no
# separate structured store, the log file *is* the record.
LOG_PATH = Path(CONFIG.get("log_file") or CONFIG_PATH.parent / "agent.log")

logger = logging.getLogger("lanhub_agent")
logger.setLevel(logging.INFO)
_handler = RotatingFileHandler(LOG_PATH, maxBytes=5 * 1024 * 1024, backupCount=3)
_handler.setFormatter(logging.Formatter("%(message)s"))
logger.addHandler(_handler)


def log_activity(method: str, path: str, status: int, client: str | None) -> None:
    logger.info(json.dumps({
        "time": datetime.now(timezone.utc).isoformat(),
        "method": method,
        "path": path,
        "status": status,
        "client": client or "-",
    }))


app = FastAPI(title="LANHub Agent")


@app.middleware("http")
async def request_logging_middleware(request: Request, call_next):
    response = await call_next(request)
    if request.url.path != "/api/health":
        target = request.url.path
        if request.url.query:
            target += f"?{request.url.query}"
        log_activity(request.method, target, response.status_code, request.client.host if request.client else None)
    return response


@app.exception_handler(HTTPException)
async def http_exception_handler(request: Request, exc: HTTPException):
    # docs/AGENT_API.md documents {"message": "..."} as the error shape;
    # FastAPI's default HTTPException handler returns {"detail": "..."}
    # instead. Override it so the hub's AgentClient (which reads the
    # "message" key) actually surfaces the real reason instead of always
    # falling back to its generic "request failed" message.
    return JSONResponse(status_code=exc.status_code, content={"message": exc.detail})


def verify_token(authorization: str | None = Header(default=None)) -> None:
    expected = f"Bearer {TOKEN}"
    if authorization != expected:
        raise HTTPException(status_code=401, detail="Invalid or missing agent token.")


def resolve_path(raw_path: str) -> Path:
    path = Path(raw_path).resolve()

    if ALLOWED_ROOTS and not any(
        path == root or root in path.parents for root in ALLOWED_ROOTS
    ):
        raise HTTPException(status_code=403, detail=f"Path {path} is outside the allowed roots.")

    return path


def is_hidden(path: Path) -> bool:
    if path.name.startswith("."):
        return True

    if platform.system() == "Windows":
        try:
            import ctypes

            attrs = ctypes.windll.kernel32.GetFileAttributesW(str(path))
            return attrs != -1 and bool(attrs & 0x2)
        except Exception:
            return False

    return False


def entry_dict(path: Path) -> dict:
    try:
        stat = path.stat()
    except OSError:
        stat = None

    return {
        "name": path.name,
        "path": str(path),
        "type": "dir" if path.is_dir() else "file",
        "size": None if (stat is None or path.is_dir()) else stat.st_size,
        "modified": (
            datetime.fromtimestamp(stat.st_mtime, tz=timezone.utc).isoformat()
            if stat
            else None
        ),
        "hidden": is_hidden(path),
    }


@app.get("/api/health")
def health(_: None = Depends(verify_token)):
    return {
        "hostname": platform.node(),
        "os": platform.system().lower(),
        "version": "0.2.0",
    }


@app.get("/api/drives")
def drives(_: None = Depends(verify_token)):
    results = []

    if platform.system() == "Windows":
        import string

        for letter in string.ascii_uppercase:
            root = f"{letter}:\\"
            if Path(root).exists():
                usage = shutil.disk_usage(root)
                results.append({
                    "path": root,
                    "label": root,
                    "free": usage.free,
                    "total": usage.total,
                })
    else:
        usage = shutil.disk_usage("/")
        results.append({"path": "/", "label": "/", "free": usage.free, "total": usage.total})

        home = Path.home()
        if home.exists():
            usage = shutil.disk_usage(str(home))
            results.append({
                "path": str(home),
                "label": str(home),
                "free": usage.free,
                "total": usage.total,
            })

    return {"drives": results}


@app.get("/api/list")
def list_directory(path: str = Query(...), _: None = Depends(verify_token)):
    directory = resolve_path(path)

    if not directory.is_dir():
        raise HTTPException(status_code=404, detail=f"{directory} is not a directory.")

    try:
        entries = [entry_dict(child) for child in directory.iterdir()]
    except PermissionError:
        raise HTTPException(status_code=403, detail=f"Permission denied reading {directory}.")

    entries.sort(key=lambda e: (e["type"] != "dir", e["name"].lower()))

    return {"path": str(directory), "entries": entries}


def _throttle_delay_for_chunk() -> float:
    return (CHUNK_SIZE / 1024) / THROTTLE_KBPS if THROTTLE_KBPS > 0 else 0


def _throttled_chunks(file_path: Path):
    delay = _throttle_delay_for_chunk()
    with file_path.open("rb") as handle:
        while True:
            chunk = handle.read(CHUNK_SIZE)
            if not chunk:
                break
            yield chunk
            if delay:
                time.sleep(delay)


@app.get("/api/download")
def download(path: str = Query(...), _: None = Depends(verify_token)):
    file_path = resolve_path(path)

    if not file_path.is_file():
        raise HTTPException(status_code=404, detail=f"{file_path} is not a file.")

    headers = {
        "Content-Disposition": f'attachment; filename="{file_path.name}"',
        "Content-Length": str(file_path.stat().st_size),
    }
    return StreamingResponse(_throttled_chunks(file_path), media_type="application/octet-stream", headers=headers)


PREVIEW_IMAGE_TYPES = {"image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp", "image/svg+xml"}
PREVIEW_TEXT_MAX_BYTES = 512 * 1024


@app.get("/api/preview")
def preview(path: str = Query(...), _: None = Depends(verify_token)):
    file_path = resolve_path(path)

    if not file_path.is_file():
        raise HTTPException(status_code=404, detail=f"{file_path} is not a file.")

    guessed_type, _encoding = mimetypes.guess_type(file_path.name)

    # X-Content-Type-Options stops a browser from re-sniffing an
    # unexpected type into something more dangerous (e.g. HTML) than the
    # header claims. Text is always served as text/plain — even a file
    # named *.html or *.svg's <script> content is inert here — since this
    # is rendered inside the hub's own origin and an arbitrary file on
    # someone's machine should never be able to execute script there.
    headers = {"X-Content-Type-Options": "nosniff"}

    if guessed_type in PREVIEW_IMAGE_TYPES:
        headers["Content-Disposition"] = f'inline; filename="{file_path.name}"'
        return StreamingResponse(_throttled_chunks(file_path), media_type=guessed_type, headers=headers)

    if guessed_type == "application/pdf":
        headers["Content-Disposition"] = f'inline; filename="{file_path.name}"'
        return StreamingResponse(_throttled_chunks(file_path), media_type="application/pdf", headers=headers)

    if guessed_type is None or guessed_type.startswith("text/") or guessed_type in {
        "application/json", "application/xml", "application/x-yaml",
    }:
        try:
            raw = file_path.open("rb").read(PREVIEW_TEXT_MAX_BYTES + 1)
        except PermissionError:
            raise HTTPException(status_code=403, detail=f"Permission denied reading {file_path}.")

        truncated = len(raw) > PREVIEW_TEXT_MAX_BYTES
        text = raw[:PREVIEW_TEXT_MAX_BYTES].decode("utf-8", errors="replace")
        if truncated:
            text += "\n\n[preview truncated]"

        return PlainTextResponse(text, headers=headers)

    raise HTTPException(status_code=415, detail=f"No preview available for {file_path.name}.")


@app.post("/api/upload")
async def upload(path: str, file: UploadFile, _: None = Depends(verify_token)):
    destination_dir = resolve_path(path)
    destination_dir.mkdir(parents=True, exist_ok=True)

    destination = destination_dir / file.filename
    delay = _throttle_delay_for_chunk()
    with destination.open("wb") as out:
        while True:
            chunk = await file.read(CHUNK_SIZE)
            if not chunk:
                break
            out.write(chunk)
            if delay:
                time.sleep(delay)

    return entry_dict(destination)


class PathBody(BaseModel):
    path: str


class RenameBody(BaseModel):
    path: str
    new_name: str


class TransferBody(BaseModel):
    source: str
    destination: str


class DeleteBody(BaseModel):
    path: str
    recursive: bool = True


@app.post("/api/mkdir")
def mkdir(body: PathBody, _: None = Depends(verify_token)):
    target = resolve_path(body.path)
    target.mkdir(parents=True, exist_ok=True)
    return entry_dict(target)


@app.post("/api/rename")
def rename(body: RenameBody, _: None = Depends(verify_token)):
    source = resolve_path(body.path)
    target = source.with_name(body.new_name)

    if target.exists():
        raise HTTPException(status_code=409, detail=f"{target} already exists.")

    source.rename(target)
    return entry_dict(target)


@app.post("/api/move")
def move(body: TransferBody, _: None = Depends(verify_token)):
    source = resolve_path(body.source)
    destination = resolve_path(body.destination)

    if destination.exists():
        raise HTTPException(status_code=409, detail=f"{destination} already exists.")

    shutil.move(str(source), str(destination))
    return entry_dict(destination)


@app.post("/api/copy")
def copy(body: TransferBody, _: None = Depends(verify_token)):
    source = resolve_path(body.source)
    destination = resolve_path(body.destination)

    if destination.exists():
        raise HTTPException(status_code=409, detail=f"{destination} already exists.")

    if source.is_dir():
        shutil.copytree(source, destination)
    else:
        shutil.copy2(source, destination)

    return entry_dict(destination)


@app.post("/api/delete")
def delete(body: DeleteBody, _: None = Depends(verify_token)):
    target = resolve_path(body.path)

    if target.is_dir():
        if not body.recursive and any(target.iterdir()):
            raise HTTPException(status_code=400, detail=f"{target} is not empty.")
        shutil.rmtree(target)
    else:
        target.unlink()

    return JSONResponse({"deleted": str(target)})


# --- Search --------------------------------------------------------------
# No index of any kind exists (or is planned) on the agent — this walks the
# tree live, bounded by depth/result-count/wall-clock so a huge or slow
# (e.g. spinning-disk, network-mounted) tree can't hang a request forever.
# Partial results are returned with truncated=true rather than erroring out.

@app.get("/api/search")
def search(
    path: str = Query(...),
    query: str = Query(..., min_length=1),
    max_depth: int = Query(8, ge=0, le=50),
    max_results: int = Query(200, ge=1, le=2000),
    timeout: float = Query(10.0, ge=1, le=60),
    _: None = Depends(verify_token),
):
    start = resolve_path(path)

    if not start.is_dir():
        raise HTTPException(status_code=404, detail=f"{start} is not a directory.")

    query_lower = query.lower()
    start_depth = len(start.parts)
    deadline = time.monotonic() + timeout
    results: list[dict] = []
    truncated = False

    for root, dirs, files in os.walk(start):
        if time.monotonic() > deadline:
            truncated = True
            break

        root_path = Path(root)
        depth = len(root_path.parts) - start_depth
        if depth >= max_depth:
            dirs[:] = []

        for name in list(dirs) + files:
            if query_lower in name.lower():
                results.append(entry_dict(root_path / name))
                if len(results) >= max_results:
                    truncated = True
                    break

        if truncated:
            break

    return {"path": str(start), "query": query, "entries": results, "truncated": truncated}


# --- Transfer queue --------------------------------------------------------
# Every existing file op (copy/move/upload/download) is synchronous,
# single-shot, per-request — fine for small files, but gives no visibility
# into a large copy in progress and blocks the request for its full
# duration. This adds an opt-in, in-process queue (no external broker —
# nothing else here has a database or message bus either) that the hub can
# poll for progress. The plain /api/copy and /api/move endpoints above are
# unchanged and still the right choice for small/instant operations.

@dataclass
class TransferJob:
    id: str
    kind: str  # "copy" or "move"
    source: str
    destination: str
    status: str = "pending"  # pending, running, done, error
    bytes_done: int = 0
    bytes_total: int = 0
    error: str | None = None
    created_at: str = field(default_factory=lambda: datetime.now(timezone.utc).isoformat())


TRANSFERS: dict[str, TransferJob] = {}
TRANSFERS_LOCK = threading.Lock()
TRANSFER_EXECUTOR = ThreadPoolExecutor(max_workers=2, thread_name_prefix="lanhub-transfer")


def _transfer_dict(job: TransferJob) -> dict:
    return {
        "id": job.id,
        "kind": job.kind,
        "source": job.source,
        "destination": job.destination,
        "status": job.status,
        "bytes_done": job.bytes_done,
        "bytes_total": job.bytes_total,
        "error": job.error,
        "created_at": job.created_at,
    }


def _directory_size(path: Path) -> int:
    if path.is_file():
        try:
            return path.stat().st_size
        except OSError:
            return 0

    total = 0
    for root, _dirs, files in os.walk(path):
        for name in files:
            try:
                total += (Path(root) / name).stat().st_size
            except OSError:
                pass
    return total


def _copy_file_with_progress(source: Path, destination: Path, job: TransferJob) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    with source.open("rb") as src, destination.open("wb") as dst:
        while True:
            chunk = src.read(CHUNK_SIZE)
            if not chunk:
                break
            dst.write(chunk)
            job.bytes_done += len(chunk)
    shutil.copystat(source, destination, follow_symlinks=True)


def _run_transfer(job_id: str) -> None:
    with TRANSFERS_LOCK:
        job = TRANSFERS[job_id]
        job.status = "running"

    try:
        source = Path(job.source)
        destination = Path(job.destination)
        job.bytes_total = _directory_size(source)

        if source.is_dir():
            for root, _dirs, files in os.walk(source):
                rel = Path(root).relative_to(source)
                for name in files:
                    _copy_file_with_progress(Path(root) / name, destination / rel / name, job)
            if job.kind == "move":
                shutil.rmtree(source)
        else:
            _copy_file_with_progress(source, destination, job)
            if job.kind == "move":
                source.unlink()

        with TRANSFERS_LOCK:
            job.status = "done"
        logger.info(json.dumps({
            "time": datetime.now(timezone.utc).isoformat(),
            "method": "TRANSFER",
            "path": f"{job.kind} {job.source} -> {job.destination}",
            "status": 200,
            "client": "-",
        }))
    except Exception as exc:  # noqa: BLE001 - report every failure back via job.error
        with TRANSFERS_LOCK:
            job.status = "error"
            job.error = str(exc)
        logger.info(json.dumps({
            "time": datetime.now(timezone.utc).isoformat(),
            "method": "TRANSFER",
            "path": f"{job.kind} {job.source} -> {job.destination}",
            "status": 500,
            "client": "-",
        }))


class TransferJobBody(BaseModel):
    source: str
    destination: str
    kind: str = "copy"  # "copy" or "move"


@app.post("/api/transfers")
def create_transfer(body: TransferJobBody, _: None = Depends(verify_token)):
    if body.kind not in ("copy", "move"):
        raise HTTPException(status_code=400, detail="kind must be 'copy' or 'move'.")

    source = resolve_path(body.source)
    destination = resolve_path(body.destination)

    if not source.exists():
        raise HTTPException(status_code=404, detail=f"{source} does not exist.")
    if destination.exists():
        raise HTTPException(status_code=409, detail=f"{destination} already exists.")

    job = TransferJob(id=uuid.uuid4().hex, kind=body.kind, source=str(source), destination=str(destination))
    with TRANSFERS_LOCK:
        TRANSFERS[job.id] = job

    TRANSFER_EXECUTOR.submit(_run_transfer, job.id)
    return _transfer_dict(job)


@app.get("/api/transfers/{transfer_id}")
def get_transfer(transfer_id: str, _: None = Depends(verify_token)):
    with TRANSFERS_LOCK:
        job = TRANSFERS.get(transfer_id)

    if not job:
        raise HTTPException(status_code=404, detail="Unknown transfer id.")

    return _transfer_dict(job)


@app.get("/api/transfers")
def list_transfers(_: None = Depends(verify_token)):
    with TRANSFERS_LOCK:
        jobs = sorted(TRANSFERS.values(), key=lambda j: j.created_at, reverse=True)[:50]

    return {"transfers": [_transfer_dict(j) for j in jobs]}


# --- Activity log ----------------------------------------------------------
# The request-logging middleware above is the only record kept — this just
# reads it back out as structured JSON for the hub's activity page. Only the
# active (non-rotated) log file is read; older, rotated entries are not
# included.

@app.get("/api/activity")
def activity(limit: int = Query(200, ge=1, le=2000), _: None = Depends(verify_token)):
    if not LOG_PATH.exists():
        return {"entries": []}

    lines = LOG_PATH.read_text(errors="replace").splitlines()[-limit:]
    entries = []
    for line in reversed(lines):
        try:
            entries.append(json.loads(line))
        except json.JSONDecodeError:
            continue

    return {"entries": entries}


if __name__ == "__main__":
    import uvicorn

    ssl_kwargs: dict = {}
    cert_file = CONFIG.get("cert_file")
    key_file = CONFIG.get("key_file")
    if cert_file and key_file:
        ssl_kwargs = {"ssl_certfile": cert_file, "ssl_keyfile": key_file}

    uvicorn.run(
        app,
        host=CONFIG.get("host", "0.0.0.0"),
        port=CONFIG.get("port", 8765),
        **ssl_kwargs,
    )
