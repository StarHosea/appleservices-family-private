"""Private pipe bridge to the pinned x86 XNU property encryption routine.

Input and output are hardware plists, never Apple account credentials. The
native ARM NAC runner calls this helper only for the short x86 property routine.
"""
import argparse
import ctypes
import plistlib
import sys
import uuid


def encrypt_hardware(hw, encrypt):
    result = {}
    for name, raw in [("platform_serial_number", hw["platform_serial_number"].encode()),
                      ("platform_uuid", uuid.UUID(hw["platform_uuid"]).bytes),
                      ("root_disk_uuid", uuid.UUID(hw["root_disk_uuid"]).bytes),
                      ("rom", hw["rom"]), ("mlb", hw["mlb"].encode())]:
        value = hw.get(name + "_enc")
        if not value:
            if not isinstance(raw, bytes) or not 1 <= len(raw) <= 1024:
                raise ValueError("invalid hardware property")
            source, target = ctypes.create_string_buffer(raw), ctypes.create_string_buffer(17)
            encrypt(source, len(raw), target)
            value = target.raw
        if not isinstance(value, bytes) or len(value) != 17:
            raise ValueError("invalid encrypted property")
        result[name + "_enc"] = value
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--library", required=True)
    args = parser.parse_args()
    data = sys.stdin.buffer.read(65537)
    if len(data) > 65536:
        raise ValueError("hardware input too large")
    hw = plistlib.loads(data)
    if sys.platform == "win32":
        ctypes.windll.kernel32.SetErrorMode(0x8003)
    library = ctypes.CDLL(args.library)
    encrypt = library.mac_io_encrypt
    encrypt.argtypes = [ctypes.c_void_p, ctypes.c_uint64, ctypes.c_void_p]
    encrypt.restype = None
    sys.stdout.buffer.write(plistlib.dumps(encrypt_hardware(hw, encrypt)))


if __name__ == "__main__":
    try:
        main()
    except Exception as failure:
        print("NAC property encryption failed: " + type(failure).__name__, file=sys.stderr)
        raise SystemExit(1)
