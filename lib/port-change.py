import contextlib
import fcntl
import hashlib
import os
import re
import stat
import subprocess
import sys
import tempfile


class PortChangeError(Exception):
    pass


def require_inactive(unit):
    result = subprocess.run(
        ["systemctl", "show", "--property=LoadState", "--property=ActiveState", "--", unit],
        stdout=subprocess.PIPE,
    )
    if result.returncode:
        raise PortChangeError(
            "could not read the state of {} (systemctl exited {})".format(unit, result.returncode)
        )

    properties = {}
    for line in result.stdout.splitlines():
        match = re.fullmatch(rb"(LoadState|ActiveState)=([a-z][a-z-]*)", line)
        if not match or match[1] in properties:
            raise PortChangeError("unexpected systemctl state for {}".format(unit))
        properties[match[1]] = match[2].decode("ascii")
    if len(properties) != 2:
        raise PortChangeError("missing systemctl state for {}".format(unit))

    load_state = properties[b"LoadState"]
    active_state = properties[b"ActiveState"]
    if load_state == "loaded" and active_state == "inactive":
        return
    message = "{} has LoadState={} and ActiveState={}".format(unit, load_state, active_state)
    if active_state in {"active", "activating", "deactivating", "reloading", "refreshing"}:
        message += ". Stop it manually with ./down.sh and run ./random.sh again"
    raise PortChangeError(message)


@contextlib.contextmanager
def open_regular(path):
    before = os.lstat(path)
    if not stat.S_ISREG(before.st_mode):
        raise PortChangeError("expected a regular file at {}".format(path))
    flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
    with os.fdopen(os.open(path, flags), "rb") as stream:
        after = os.fstat(stream.fileno())
        if not stat.S_ISREG(after.st_mode) or not os.path.samestat(before, after):
            raise PortChangeError("file changed while opening {}".format(path))
        yield stream


def read_verified(path, digest):
    with open_regular(path) as stream:
        content = stream.read()
    if hashlib.sha256(content).hexdigest() != digest:
        raise PortChangeError("the preview files changed. Run ./random.sh again")
    return content


def verify_current(path, stream, original, metadata):
    stream.seek(0)
    content = stream.read()
    current = os.lstat(path)
    if (
        content != original
        or not os.path.samestat(metadata, current)
        or (metadata.st_mode, metadata.st_uid, metadata.st_gid)
        != (current.st_mode, current.st_uid, current.st_gid)
    ):
        raise PortChangeError("{} changed since the preview. Run ./random.sh again".format(path))


def write_staged(path, content, metadata=None):
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL
    with os.fdopen(os.open(path, flags, 0o600), "wb") as stream:
        stream.write(content)
        stream.flush()
        if metadata is not None:
            os.fchown(stream.fileno(), metadata.st_uid, metadata.st_gid)
        mode = 0o600 if metadata is None else stat.S_IMODE(metadata.st_mode)
        os.fchmod(stream.fileno(), mode)
        os.fsync(stream.fileno())


def commit(unit, config, original_file, candidate_file, original_digest, candidate_digest):
    original = read_verified(original_file, original_digest)
    candidate = read_verified(candidate_file, candidate_digest)
    with open_regular(config) as current:
        try:
            fcntl.flock(current.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise PortChangeError("another port update holds the lock for {}".format(config))
        metadata = os.fstat(current.fileno())
        verify_current(config, current, original, metadata)

        parent = os.path.dirname(os.path.abspath(config))
        with tempfile.TemporaryDirectory(prefix=".port-change.", dir=parent) as staging:
            os.chmod(staging, 0o700)
            staged_backup = os.path.join(staging, "original")
            staged_config = os.path.join(staging, "candidate")
            write_staged(staged_backup, original)
            write_staged(staged_config, candidate, metadata)

            require_inactive(unit)
            verify_current(config, current, original, metadata)
            suffix = os.path.basename(staging).rsplit(".", 1)[1]
            backup = "{}.bak.{}".format(config, suffix)
            os.link(staged_backup, backup)
            try:
                os.replace(staged_config, config)
            except OSError:
                os.unlink(backup)
                raise

    print(backup)


def main(args):
    try:
        if len(args) == 2 and args[0] == "check":
            require_inactive(args[1])
        elif len(args) == 2 and args[0] == "snapshot":
            with open_regular(args[1]) as source:
                sys.stdout.buffer.write(source.read())
        elif len(args) == 7 and args[0] == "commit":
            commit(*args[1:])
        else:
            raise PortChangeError("invalid internal port-change invocation")
    except (OSError, PortChangeError) as error:
        print("random.sh: {}".format(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
