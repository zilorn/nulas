import unittest
import tempfile
import json
import shutil
import subprocess
from pathlib import Path
from unittest.mock import patch
import install_service
import setup
from install_service import service_text, unit_quote, working_directory, uninstall_service


class ServiceTests(unittest.TestCase):
    def test_paths_are_systemd_escaped(self):
        self.assertEqual(unit_quote('/path/50% "test"'), '"/path/50%% \\"test\\""')
        with self.assertRaises(ValueError):
            unit_quote('/path\nExecStart=bad')

    def test_service_starts_both_servers_via_supervisor(self):
        unit = service_text(Path('/srv/nulas'), '/opt/node/bin/node')
        self.assertIn('ExecStart=/bin/bash "/srv/nulas/scripts/start.sh"', unit)
        self.assertIn('KillMode=control-group', unit)
        self.assertIn('WantedBy=default.target', unit)
        self.assertIn('WorkingDirectory=/srv/nulas\n', unit)

    def test_dollars_are_escaped_only_in_exec_arguments(self):
        root = Path('/srv/$USER/${HOME}/cash$$ 50%')
        unit = service_text(root, '/opt/$NODE/bin/node')
        self.assertIn('ExecStart=/bin/bash "/srv/$$USER/$${HOME}/cash$$$$ 50%%/scripts/start.sh"\n', unit)
        self.assertIn('WorkingDirectory=/srv/$USER/${HOME}/cash$$ 50%%\n', unit)
        self.assertIn('Environment="PATH=/opt/$NODE/bin:/usr/local/bin:/usr/bin:/bin"\n', unit)
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory) / '$USER ${HOME} cash$$ 50%'
            home.mkdir()
            python = '/opt/$PYTHON/${VERSION}/python3'
            (home / 'installation.json').write_text(json.dumps({'tools': {'python': python}}))
            unit = service_text(root, '/usr/bin/node', home)
            self.assertIn('ExecStart=' + unit_quote(python) + ' "' + str(home).replace('$', '$$').replace('%', '%%') + '/bin/launcher.py" run\n', unit)
            self.assertNotIn('ExecStart=:', unit)

    def test_working_directory_is_literal_with_specifiers_escaped(self):
        self.assertEqual(working_directory('/srv/50% "test"\\folder'), '/srv/50%% "test"\\folder')
        for path in ('relative/path', '/srv/\nExecStart=bad', '/srv/\r', '/srv/\x00', '/srv/end ', '/srv/end\\'):
            with self.subTest(path=path), self.assertRaises(ValueError):
                working_directory(path)

    @unittest.skipUnless(shutil.which('systemd-analyze'), 'systemd-analyze is unavailable')
    def test_generated_service_passes_systemd_verification(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'release $USER ${HOME} $$ with spaces 50% "quoted"'
            (root / 'scripts').mkdir(parents=True)
            (root / 'scripts/start.sh').write_text('#!/bin/bash\nexit 0\n')
            unit = Path(directory) / 'nulas.service'
            unit.write_text(service_text(root, '/usr/bin/node'))
            result = subprocess.run(['systemd-analyze', 'verify', str(unit)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_managed_service_ownership_accepts_old_and_new_exec_syntax(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory) / '$USER ${HOME} $$ 50% install'
            home.mkdir()
            (home / 'installation.json').write_text(json.dumps({'tools': {'python': '/usr/bin/python3'}}))
            config = Path(directory) / 'config'
            unit = config / 'systemd/user/nulas.service'
            unit.parent.mkdir(parents=True)
            for legacy in (False, True):
                with self.subTest(legacy=legacy):
                    content = service_text(Path('/old/release'), '/usr/bin/node', home)
                    if legacy:
                        content = '\n'.join(
                            'ExecStart=:' + unit_quote('/usr/bin/python3') + ' ' + unit_quote(str(home / 'bin/launcher.py')) + ' run'
                            if line.startswith('ExecStart=') else line for line in content.split('\n'))
                    unit.write_text(content)
                    with patch.dict(install_service.os.environ, {'XDG_CONFIG_HOME': str(config)}), \
                            patch.object(install_service.sys, 'platform', 'linux'), \
                            patch.object(install_service.os, 'geteuid', return_value=1000), \
                            patch.object(setup, 'command'), \
                            patch.object(install_service.subprocess, 'check_output', return_value=str(unit)), \
                            patch.object(install_service.subprocess, 'run') as run:
                        setup.migrate_service_directory(home)
                        with self.assertRaisesRegex(RuntimeError, 'different installation'):
                            uninstall_service(home / 'other')
                        run.assert_not_called()
                        uninstall_service(home)
                        self.assertFalse(unit.exists())


    def test_managed_service_resolves_current_release_at_start(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            (home / 'installation.json').write_text(json.dumps({'tools': {'python': '/usr/bin/python3'}}))
            unit = service_text(Path('/old/release'), '/opt/node/bin/node', home)
            self.assertIn('"' + str(home / 'bin/launcher.py') + '" run', unit)
            self.assertNotIn('scripts/start.sh', unit)
            self.assertIn('WorkingDirectory=' + str(home) + '\n', unit)
            self.assertIn('KillMode=control-group', unit)

    def test_cleanup_migrates_old_managed_service_before_reload(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory) / 'install'
            home.mkdir()
            (home / 'installation.json').write_text(json.dumps({'tools': {'python': '/usr/bin/python3'}}))
            config = Path(directory) / 'config'
            unit = config / 'systemd/user/nulas.service'
            unit.parent.mkdir(parents=True)
            old = home / 'releases/old'
            content = service_text(old, '/usr/bin/node', home).replace(
                'WorkingDirectory=' + str(home), 'WorkingDirectory=' + str(old))
            unit.write_text(content)
            def reload(*args, **kwargs):
                self.assertEqual(args, ('systemctl', '--user', 'daemon-reload'))
                self.assertEqual(kwargs['timeout'], 10)
                self.assertIn('WorkingDirectory=' + str(home) + '\n', unit.read_text())
                self.assertIn('EnvironmentFile=-%h/.config/nulas/service.env', unit.read_text())
            with patch.dict(setup.os.environ, {'XDG_CONFIG_HOME': str(config)}), \
                    patch.object(setup.sys, 'platform', 'linux'), \
                    patch.object(setup, 'command', side_effect=reload) as command:
                setup.migrate_service_directory(home)
                setup.migrate_service_directory(home)
                self.assertEqual(command.call_count, 2)
                unit.write_text('[Service]\nWorkingDirectory=/other\n')
                with self.assertRaisesRegex(RuntimeError, 'ownership'):
                    setup.migrate_service_directory(home)
                self.assertEqual(command.call_count, 2)

    def test_uninstall_checks_ownership_and_stops_before_deleting(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory)
            unit = config / 'systemd/user/nulas.service'
            unit.parent.mkdir(parents=True)
            with patch.dict(install_service.os.environ, {'XDG_CONFIG_HOME': str(config)}), \
                    patch.object(install_service.sys, 'platform', 'linux'), \
                    patch.object(install_service.os, 'geteuid', return_value=1000), \
                    patch.object(install_service.subprocess, 'check_output', return_value=str(unit)) as query, \
                    patch.object(install_service.subprocess, 'run') as run:
                uninstall_service()  # absent unit: no system commands
                run.assert_not_called()
                query.assert_not_called()
                unit.write_text('[Unit]\nDescription=Other\n')
                with self.assertRaisesRegex(RuntimeError, 'not created by Nulas'):
                    uninstall_service()
                query.assert_not_called()
                unit.write_text(service_text(Path('/srv/nulas'), '/usr/bin/node'))
                with patch.object(install_service.subprocess, 'check_output', return_value='/other/nulas.service'):
                    with self.assertRaisesRegex(RuntimeError, 'different loaded'):
                        uninstall_service()
                run.assert_not_called()
                run.side_effect = subprocess.CalledProcessError(1, 'systemctl')
                with self.assertRaises(subprocess.CalledProcessError):
                    uninstall_service()
                self.assertTrue(unit.exists())
                run.reset_mock()
                run.side_effect = None
                def verify(args, **kwargs):
                    self.assertEqual(unit.exists(), args[2] == 'disable')
                run.side_effect = verify
                uninstall_service()
                self.assertFalse(unit.exists())
                self.assertEqual([c.args[0] for c in run.call_args_list], [
                    ['systemctl', '--user', 'disable', '--now', 'nulas.service'],
                    ['systemctl', '--user', 'daemon-reload']])

    def test_remove_refuses_service_from_another_installation(self):
        with tempfile.TemporaryDirectory() as directory:
            config = Path(directory)
            unit = config / 'systemd/user/nulas.service'
            unit.parent.mkdir(parents=True)
            unit.write_text(service_text(Path('/srv/nulas'), '/usr/bin/node'))
            with patch.dict(install_service.os.environ, {'XDG_CONFIG_HOME': str(config)}), \
                    patch.object(install_service.sys, 'platform', 'linux'), \
                    patch.object(install_service.os, 'geteuid', return_value=1000), \
                    patch.object(install_service.subprocess, 'run') as run:
                with self.assertRaisesRegex(RuntimeError, 'different installation'):
                    uninstall_service(Path('/another/install'))
                run.assert_not_called()
                self.assertTrue(unit.exists())


if __name__ == '__main__':
    unittest.main()
