import re
import sys


LISTEN_PORT = re.compile(rb"\s*([0-9]+)\s*")
IPTABLES = re.compile(
    rb"\s*iptables\s+(-[ID])\s+INPUT\s+-p\s+udp\s+--dport\s+([0-9]+)\s+-j\s+ACCEPT\s*"
)
FIREWALLD = re.compile(
    rb"\s*firewall-cmd\s+--zone=public\s+--add-interface=[A-Za-z0-9_=+.-]+\s*&&\s*"
    rb"firewall-cmd\s+(--(?:add|remove)-port)\s+([0-9]+)/udp"
    rb"(?=\s*&&\s*firewall-cmd\s+--add-rich-rule=)"
)
HOOK_ACTIONS = {
    b"postup": (b"-I", b"--add-port"),
    b"postdown": (b"-D", b"--remove-port"),
}


def decimal_port(digits):
    significant = digits.lstrip(b"0")
    if not significant.isdigit() or len(significant) > 5:
        return None
    port = int(significant)
    return port if port <= 65535 else None


def interface_fields(lines):
    in_interface = False
    for index, line in enumerate(lines):
        key, _, value = line.partition(b"#")[0].partition(b"=")
        name = key.strip().lower()
        if name.startswith(b"["):
            in_interface = name == b"[interface]"
        elif in_interface:
            yield index, name, len(key) + 1, value


def find_listen_port(fields):
    matches = [field for field in fields if field[1] == b"listenport"]
    if len(matches) != 1:
        raise ValueError("expected exactly one ListenPort in [Interface]")

    index, _, offset, value = matches[0]
    match = LISTEN_PORT.fullmatch(value)
    port = decimal_port(match[1]) if match else None
    if port is None:
        raise ValueError("ListenPort must be a decimal port from 1 to 65535")

    return port, index, offset + match.start(1), offset + match.end(1)


def replace_ports(lines, fields, current, old, new):
    port, index, start, end = current
    if old is None or old != port:
        raise ValueError("old port must match the configured ListenPort")
    if new is None or new < 10000 or new == old:
        raise ValueError("new port must be different and in the range 10000 to 65535")

    replacement = str(new).encode("ascii")
    lines[index] = lines[index][:start] + replacement + lines[index][end:]
    for index, name, offset, value in fields:
        if name not in HOOK_ACTIONS:
            continue
        match = IPTABLES.fullmatch(value) or FIREWALLD.match(value)
        if match and match[1] in HOOK_ACTIONS[name] and decimal_port(match[2]) == old:
            start, end = offset + match.start(2), offset + match.end(2)
            lines[index] = lines[index][:start] + replacement + lines[index][end:]
    return b"\n".join(lines)


def main(args):
    read_only = len(args) == 2 and args[0] == "port"
    replace = len(args) == 4 and args[0] == "replace"
    if not (read_only or replace):
        print("Usage: wg-config.py port <file> | replace <file> <old> <new>", file=sys.stderr)
        return 1

    try:
        with open(args[1], "rb") as config:
            content = config.read()
        if b"\x00" in content:
            raise ValueError("configuration contains a NUL byte")
        lines = content.split(b"\n")
        fields = list(interface_fields(lines))
        current = find_listen_port(fields)
        if read_only:
            print(current[0])
        else:
            old, new = [decimal_port(arg.encode("ascii", errors="replace")) for arg in args[2:]]
            sys.stdout.buffer.write(replace_ports(lines, fields, current, old, new))
    except (OSError, ValueError) as error:
        print("wg-config.py: {}".format(error), file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
