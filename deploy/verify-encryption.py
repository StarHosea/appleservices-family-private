"""Check upstream XNU known-answer vectors, without creating hardware state."""
import base64
import ctypes
import sys
import uuid

library = ctypes.CDLL(sys.argv[1])
encrypt = library.mac_io_encrypt
encrypt.argtypes = [ctypes.c_void_p, ctypes.c_uint64, ctypes.c_void_p]
encrypt.restype = None
# Public upstream test fixtures, used only for cryptographic conformance.
vectors = [
    (b"C02YT1YMJK7M", "c3kZ7+WofxcjaBTInJCwSV0="),
    (uuid.UUID("11D299A5-CF0B-544D-BAD3-7AC7A6E452D7").bytes, "jGguP3mQH+Vw6dMAWrqZOnk="),
    (uuid.UUID("FCDB63B5-D208-4AEE-B368-3DE952B911FF").bytes, "VvJODAsuSRdGQlhB5kPgf2M="),
    (base64.b64decode("V9BNndaG"), "wWF12gciXzN/96bIt/ufTB0="),
    (b"C02923200KVKN3YAG", "CKp4ROiInBYAdvnbrbNjjkM="),
]
for value, expected in vectors:
    source, destination = ctypes.create_string_buffer(value), ctypes.create_string_buffer(17)
    encrypt(source, len(value), destination)
    if destination.raw != base64.b64decode(expected):
        raise RuntimeError("XNU property routine failed a known-answer vector")
print("XNU property routine: all five upstream vectors passed")
