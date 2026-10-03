import unittest
import tempfile
import json
import shutil
import subprocess
from pathlib import Path
from install_service import service_text, unit_quote, working_directory


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
        self.assertIn('WorkingDirectory=/srv/nulas\n', unit)

    def test_working_directory_is_literal_with_specifiers_escaped(self):
        self.assertEqual(working_directory('/srv/50% "test"\\folder'), '/srv/50%% "test"\\folder')
        for path in ('relative/path', '/srv/\nExecStart=bad', '/srv/\r', '/srv/\x00', '/srv/end ', '/srv/end\\'):
            with self.subTest(path=path), self.assertRaises(ValueError):
                working_directory(path)

    @unittest.skipUnless(shutil.which('systemd-analyze'), 'systemd-analyze is unavailable')
    def test_generated_service_passes_systemd_verification(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'release with spaces 50% "quoted"'
            (root / 'scripts').mkdir(parents=True)
            (root / 'scripts/start.sh').write_text('#!/bin/bash\nexit 0\n')
            unit = Path(directory) / 'nulas.service'
            unit.write_text(service_text(root, '/usr/bin/node'))
            result = subprocess.run(['systemd-analyze', 'verify', str(unit)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


    def test_managed_service_resolves_current_release_at_start(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            (home / 'installation.json').write_text(json.dumps({'tools': {'python': '/usr/bin/python3'}}))
            unit = service_text(Path('/old/release'), '/opt/node/bin/node', home)
            self.assertIn('"' + str(home / 'bin/launcher.py') + '" run', unit)
            self.assertNotIn('scripts/start.sh', unit)
            self.assertIn('KillMode=control-group', unit)


if __name__ == '__main__':
    unittest.main()
