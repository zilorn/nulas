import gzip
import io
import unittest
import zipfile
from install_core import asset_name, unpack


class InstallerTests(unittest.TestCase):
    def test_platform_selection(self):
        self.assertEqual(asset_name("Linux", "x86_64", "v1.19.0"), "mihomo-linux-amd64-compatible-v1.19.0.gz")
        self.assertEqual(asset_name("Darwin", "arm64", "v1.19.0"), "mihomo-darwin-arm64-v1.19.0.gz")
        self.assertEqual(asset_name("Windows", "AMD64", "v1.19.0"), "mihomo-windows-amd64-v1.19.0.zip")
        with self.assertRaises(ValueError):
            asset_name("Linux", "riscv", "v1.19.0")

    def test_archive_extraction(self):
        self.assertEqual(unpack(gzip.compress(b"binary"), "Linux"), b"binary")
        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            archive.writestr("../mihomo.exe", b"binary")
        self.assertEqual(unpack(buffer.getvalue(), "Windows"), b"binary")


if __name__ == "__main__":
    unittest.main()
