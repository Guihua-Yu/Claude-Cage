"""Bounded guest probes. Output contains observations, never file contents."""
import base64
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
    result["ipv6_connected"] = connected(("2606:4700:4700::1111", 443), socket.AF_INET6)
    try:
        routes = subprocess.run(["ip", "-j", "-6", "route", "show", "default"], capture_output=True, text=True, timeout=3, check=True)
        result["ipv6_default_routes"] = len(json.loads(routes.stdout))
    except (OSError, ValueError, subprocess.SubprocessError):
        result["ipv6_default_routes"] = -1
    udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try:
        udp.sendto(b"Cage network boundary probe", ("1.1.1.1", 3478))
        result["udp_attempted"] = True
    except OSError:
        result["udp_attempted"] = False
    finally:
        udp.close()
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
