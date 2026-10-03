import unittest
from pathlib import Path
from install_service import service_text, unit_quote


class ServiceTests(unittest.TestCase):
    def test_paths_are_systemd_escaped(self):
        self.assertEqual(unit_quote('/path/50% "test"'), '"/path/50%% \\"test\\""')
        with self.assertRaises(ValueError):
            unit_quote('/path\nExecStart=bad')

    def test_service_starts_both_servers_via_supervisor(self):
        unit = service_text(Path('/srv/nulas'), '/opt/node/bin/node')
        self.assertIn('ExecStart=:/bin/bash "/srv/nulas/scripts/start.sh"', unit)
        self.assertIn('KillMode=control-group', unit)
        self.assertIn('WantedBy=default.target', unit)


if __name__ == '__main__':
    unittest.main()
