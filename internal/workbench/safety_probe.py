"""Bounded guest probes. Output contains observations, never file contents."""
import base64
import ipaddress
import json
import locale
import os
from pathlib import Path
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request

args = json.loads(base64.b64decode(sys.argv[1]))
result = {}


def connected(address, family=socket.AF_INET):
    try:
        connection = socket.socket(family, socket.SOCK_STREAM)
    except OSError:
        return False
    connection.settimeout(1.5)
    try:
        connection.connect(address)
        return True
    except OSError:
        return False
    finally:
        connection.close()


def udp_attempted(address, payload):
    # A datagram leaving the guest is only evidence that the probe ran; the
    # host network layer records whether it was denied.
    udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        udp.sendto(payload, address)
        return True
    except OSError:
        return False
    finally:
        udp.close()


def probe(path):
    # Environment variables must not redirect the probe around the VM network.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    request = urllib.request.Request("http://198.18.0.254" + path)
    try:
        with opener.open(request, timeout=16) as response:
            return {"status": response.status, **json.loads(response.read(4096))}
    except urllib.error.HTTPError as error:
        return {"status": error.code}
    except (OSError, ValueError):
        return {"status": 0}


def ip_json(*arguments):
    output = subprocess.run(["ip", "-j", *arguments], capture_output=True, text=True, timeout=3, check=True)
    return json.loads(output.stdout)


def timezone_observation(expected):
    # The environment variable and /etc/timezone are configuration; the
    # effective UTC offset and the /etc/localtime link are what programs use.
    observation = {"localtime_target": "", "utc_offset_actual": time.localtime().tm_gmtoff}
    try:
        observation["localtime_target"] = os.readlink("/etc/localtime")
    except OSError:
        pass
    if expected:
        try:
            from datetime import datetime, timezone
            from zoneinfo import ZoneInfo
            offset = datetime.now(timezone.utc).astimezone(ZoneInfo(expected)).utcoffset()
            observation["utc_offset_expected"] = int(offset.total_seconds())
        except Exception:  # unknown zone, missing tzdata or zoneinfo module
            pass
    return observation


def resolver_observation(management_host):
    # Every configured resolver must stay inside the VM network: a loopback
    # stub or the gateway network served by the host network layer. A host LAN
    # or public resolver would carry queries around the encrypted DNS path.
    servers = set()
    files = ("/etc/resolv.conf", "/run/systemd/resolve/resolv.conf")
    readable = 0
    for name in files:
        try:
            text = Path(name).read_text()
        except OSError:
            continue
        readable += 1
        for line in text.splitlines():
            fields = line.split()
            if len(fields) >= 2 and fields[0] == "nameserver":
                servers.add(fields[1].split("%")[0])
    if not readable:
        return {"count": -1, "confined": False}
    allowed = [ipaddress.ip_network("198.18.0.0/15")]
    try:
        allowed.append(ipaddress.ip_network(management_host + "/24", strict=False))
    except ValueError:
        pass
    confined = bool(servers)
    for server in servers:
        try:
            address = ipaddress.ip_address(server)
        except ValueError:
            confined = False
            continue
        if not address.is_loopback and not any(address in network for network in allowed):
            confined = False
    return {"count": len(servers), "confined": confined}


def vsock_observation():
    # VSOCK bypasses the virtual network card entirely. Lima's guest agent
    # listens on port 2222 for the host; the host must not listen for guests.
    family = getattr(socket, "AF_VSOCK", 40)
    try:
        channel = socket.socket(family, socket.SOCK_STREAM)
    except OSError:
        return "unsupported"
    channel.settimeout(1.5)
    try:
        channel.connect((2, 2222))
        return "connected"
    except OSError:
        return "refused"
    finally:
        channel.close()


if args["stage"] in ("boundary", "environment"):
    result.update(tz=os.environ.get("TZ", ""), lang=os.environ.get("LANG", ""), lc_all=os.environ.get("LC_ALL", ""))
    try:
        result["system_timezone"] = Path("/etc/timezone").read_text().strip()
        locale.setlocale(locale.LC_ALL, "")
        result["locale_valid"] = True
    except (OSError, locale.Error):
        result["locale_valid"] = False
    result.update(timezone_observation(args.get("timezone", "")))

if args["stage"] == "boundary":
    result["public_connected"] = connected(("1.1.1.1", 443))
    result["management_connected"] = connected((args["management_host"], args["control_port"]))
    result["link_local_connected"] = connected(("169.254.169.254", 80))
    if args.get("proxy_host") and args.get("proxy_port"):
        result["proxy_endpoint_connected"] = connected((args["proxy_host"], int(args["proxy_port"])))
    result["ipv6_connected"] = connected(("2606:4700:4700::1111", 443), socket.AF_INET6)
    try:
        result["ipv6_default_routes"] = len(ip_json("-6", "route", "show", "default"))
    except (OSError, ValueError, subprocess.SubprocessError):
        result["ipv6_default_routes"] = -1
    try:
        result["ipv6_global_addresses"] = sum(len(link.get("addr_info", [])) for link in ip_json("-6", "addr", "show", "scope", "global"))
    except (OSError, ValueError, subprocess.SubprocessError, AttributeError):
        result["ipv6_global_addresses"] = -1
    try:
        result["interfaces"] = sorted(name for _, name in socket.if_nameindex() if name != "lo")
    except OSError:
        result["interfaces"] = []
    result["udp_attempted"] = udp_attempted(("1.1.1.1", 3478), b"Cage network boundary probe")
    result["dns_udp_attempted"] = udp_attempted(("1.1.1.1", 53), b"Cage DNS boundary probe")
    result["dns_tcp_connected"] = connected(("1.1.1.1", 53))
    result["dot_connected"] = connected(("1.1.1.1", 853))
    result["resolvers"] = resolver_observation(args["management_host"])
    result["vsock"] = vsock_observation()
    try:
        socket.getaddrinfo(args["nonce"] + ".example.com", 443)
        result["public_dns_answered"] = True
    except socket.gaierror:
        result["public_dns_answered"] = False
    root_probe = """import os,sys
try:
 fd=os.open(sys.argv[1],os.O_RDONLY|os.O_NONBLOCK)
 os.close(fd)
 print('readable')
except (FileNotFoundError,PermissionError): print('unreachable')
except OSError: print('unknown')
"""
    result["private_file"] = "unknown"
    try:
        check = subprocess.run(["sudo", "-n", "python3", "-c", root_probe, args["private_file"]], capture_output=True, text=True, timeout=3, check=True)
        result["private_file"] = check.stdout.strip()
    except (OSError, subprocess.SubprocessError):
        pass
    result["shared_file"] = False
    try:
        fd = os.open(args["shared_file"], os.O_RDWR | os.O_NOFOLLOW)
        with os.fdopen(fd, "r+") as shared:
            if shared.read(256) == args["challenge"]:
                shared.seek(0)
                shared.write(args["challenge"] + ":guest")
                shared.truncate()
                shared.flush()
                result["shared_file"] = True
    except OSError:
        pass

if args["stage"] == "network":
    result["egress"] = probe("/egress")
    result["dns"] = probe("/dns")

print(json.dumps(result))
