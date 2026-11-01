import ctypes
import importlib.util
from pathlib import Path
import unittest


spec = importlib.util.spec_from_file_location("nac_encrypt", Path(__file__).with_name("mac-nac-encrypt.py"))
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)


class PropertyBridgeTest(unittest.TestCase):
    def hardware(self):
        return {"platform_serial_number": "TESTSERIAL", "platform_uuid": "00000000-0000-0000-0000-000000000001",
                "root_disk_uuid": "00000000-0000-0000-0000-000000000002", "rom": bytes(range(6)), "mlb": "TESTBOARD"}

    def test_binary_properties_and_existing_encryption_survive_the_bridge(self):
        hardware = self.hardware()
        hardware["rom_enc"] = b"r" * 17
        inputs = []

        def encrypt(source, length, target):
            inputs.append(ctypes.string_at(source, length))
            ctypes.memmove(target, b"e" * 17, 17)

        result = helper.encrypt_hardware(hardware, encrypt)
        self.assertEqual(result["rom_enc"], b"r" * 17)
        self.assertEqual(len(inputs), 4)
        self.assertIn(bytes(15) + b"\x01", inputs)
        self.assertIn(bytes(15) + b"\x02", inputs)
        self.assertEqual(set(result), {name + "_enc" for name in hardware if not name.endswith("_enc")})
        self.assertTrue(all(len(value) == 17 for value in result.values()))

    def test_malformed_existing_encryption_is_rejected(self):
        hardware = self.hardware()
        hardware["platform_serial_number_enc"] = "private-invalid-property"
        with self.assertRaises(ValueError):
            helper.encrypt_hardware(hardware, lambda *_: self.fail("invalid encryption was used"))


if __name__ == "__main__":
    unittest.main()
