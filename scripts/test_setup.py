import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import setup


class SetupTests(unittest.TestCase):
    def test_pipe_bootstrap_preserves_arguments_without_network_or_installing(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            log = root / 'arguments.json'
            python = root / 'python3'
            python.write_text('#!' + sys.executable + '\n' +
                              "import json, os, pathlib, sys\n"
                              "if sys.argv[1] == '-':\n"
                              "    sys.stdin.read()\n"
                              "    pathlib.Path(sys.argv[2]).write_text('download fixture')\n"
                              "elif sys.argv[1] != '-c':\n"
                              "    pathlib.Path(os.environ['SETUP_TEST_LOG']).write_text(json.dumps(sys.argv[1:]))\n")
            python.chmod(0o755)
            environment = dict(os.environ, PATH=str(root) + os.pathsep + os.environ['PATH'], SETUP_TEST_LOG=str(log))
            script = (Path(__file__).parent / 'install.sh').read_text()
            result = subprocess.run(['bash', '-s', '--', '--home', str(root / 'app')], input=script,
                                    text=True, cwd=root, env=environment, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            args = json.loads(log.read_text())
            self.assertEqual(args[1:], ['install', '--home', str(root / 'app')])
            self.assertFalse(Path(args[0]).exists())  # bootstrap temporary directory cleaned

    def test_stable_launcher_reads_latest_pointer_and_preserves_data_path(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            tools = {key: sys.executable for key in ('git', 'go', 'node', 'python')}
            setup.launchers(home)
            for version in ('first', 'second'):
                release = home / version
                (release / 'scripts').mkdir(parents=True)
                (release / 'scripts/setup.py').write_text(
                    "import json, os, sys\nprint(json.dumps([" + repr(version) +
                    ", sys.argv[1:], os.environ['NULAS_DATA_DIR']]))\n")
                setup.atomic_json(home / 'installation.json', {'current': str(release), 'tools': tools})
                environment = dict(os.environ)
                environment.pop('NULAS_DATA_DIR', None)
                result = subprocess.check_output([str(home / 'bin/nulas'), 'update', '--check'],
                                                 text=True, env=environment)
                value = json.loads(result)
                self.assertEqual(value, [version, ['update', '--check', '--home', str(home)], str(home / 'data')])

    def test_reuses_existing_dependencies(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(setup, 'version_ok', return_value=True), patch.object(setup.shutil, 'which', side_effect=lambda name: '/existing/' + name), patch.object(setup, 'download') as fetch, patch.object(setup, 'package_install') as install:
            tools = setup.dependencies(Path(temporary))
            self.assertEqual(tools['node'], '/existing/node')
            self.assertEqual(tools['go'], '/existing/go')
            fetch.assert_not_called()
            install.assert_not_called()

    def test_atomic_failure_preserves_current(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            manifest = home / 'installation.json'
            setup.atomic_json(manifest, {'current': 'old', 'autoUpdate': False})
            with patch.object(setup.os, 'replace', side_effect=OSError('disk error')):
                with self.assertRaises(OSError):
                    setup.atomic_json(manifest, {'current': 'new'})
            self.assertEqual(json.loads(manifest.read_text())['current'], 'old')
            self.assertEqual(list(home.glob('.pending-*')), [])

    def test_lock_rejects_concurrent_updates(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            with setup.locked(home):
                with self.assertRaises(RuntimeError):
                    with setup.locked(home):
                        pass
            self.assertFalse((home / '.update-lock').exists())

    def test_shell_paths_preserve_existing_content_and_are_idempotent(self):
        with tempfile.TemporaryDirectory() as temporary, patch.dict(os.environ, {}, clear=True):
            home = Path(temporary)
            (home / '.bashrc').write_text('alias keep=true\n')
            (home / '.bash_profile').write_text('# existing login\n')
            install = home / "application space'quote"
            (install / 'bin').mkdir(parents=True)
            setup.add_path(install, home)
            first = (home / '.bashrc').read_text()
            setup.add_path(install, home)
            self.assertEqual(first, (home / '.bashrc').read_text())
            self.assertTrue(first.startswith('alias keep=true\n'))
            self.assertIn('Nulas CLI', (home / '.bash_profile').read_text())
            self.assertIn('fish_add_path', (home / '.config/fish/conf.d/nulas.fish').read_text())
            result = subprocess.check_output(['sh', '-c', '. "$1"; printf "%s" "$PATH"', 'sh', str(home / '.profile')], text=True)
            self.assertTrue(result.startswith(str(install / 'bin') + ':'))
            if setup.shutil.which('fish'):
                output = subprocess.check_output(['fish', '--no-config', '-c',
                    'source "$argv[1]"; printf "%s\\n" $PATH',
                    str(home / '.config/fish/conf.d/nulas.fish')], text=True)
                self.assertEqual(output.splitlines()[0], str(install / 'bin'))

    def test_archive_digest_traversal_and_links(self):
        def archive(name, link=None):
            output = io.BytesIO()
            with tarfile.open(fileobj=output, mode='w:gz') as package:
                member = tarfile.TarInfo(name)
                if link:
                    member.type = tarfile.SYMTYPE
                    member.linkname = link
                    package.addfile(member)
                else:
                    member.size = 2
                    package.addfile(member, io.BytesIO(b'ok'))
            return output.getvalue()
        with tempfile.TemporaryDirectory() as temporary:
            for name, link in [('../escape', None), ('safe/link', '../../escape')]:
                data = archive(name, link)
                with self.assertRaises(RuntimeError):
                    setup.unpack(data, hashlib.sha256(data).hexdigest(), Path(temporary) / 'tools', '.tar.gz')
            data = archive('tool/file')
            with self.assertRaises(RuntimeError):
                setup.unpack(data, '0' * 64, Path(temporary) / 'tools', '.tar.gz')
            setup.unpack(data, hashlib.sha256(data).hexdigest(), Path(temporary) / 'tools', '.tar.gz')
            self.assertEqual((Path(temporary) / 'tools/tool/file').read_text(), 'ok')

    def test_update_stages_commit_and_failed_build_keeps_current(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            remote = home / 'remote'
            remote.mkdir()
            def git(*args, cwd=remote):
                return setup.command('git', *args, cwd=cwd, capture=True)
            git('init', '-b', 'main')
            git('config', 'user.name', 'Test')
            git('config', 'user.email', 'test@example.invalid')
            (remote / 'file').write_text('first')
            git('add', '.')
            git('commit', '-m', 'first')
            first = git('rev-parse', 'HEAD')
            git('clone', str(remote), str(home / 'source'))
            manifest = {'commit': first, 'current': 'original', 'branch': 'main', 'tools': {'git': '/usr/bin/git', 'node': '/usr/bin/node', 'go': '/usr/bin/go'}}
            setup.atomic_json(home / 'installation.json', manifest)
            (remote / 'file').write_text('second')
            git('commit', '-am', 'second')
            second = git('rev-parse', 'HEAD')
            with patch.object(setup, 'build', side_effect=RuntimeError('build failed')):
                with self.assertRaises(RuntimeError):
                    setup.update(home)
            self.assertEqual(setup.load(home)['commit'], first)
            with patch.object(setup, 'build') as build:
                setup.update(home, check=True)
                build.assert_not_called()
                setup.update(home)
            self.assertEqual(setup.load(home)['commit'], second)
            self.assertEqual((Path(setup.load(home)['current']) / 'file').read_text(), 'second')
            self.assertEqual((remote / 'file').read_text(), 'second')
            # A force-pushed/divergent history must never become active.
            git('checkout', '--orphan', 'rewrite')
            git('commit', '-am', 'rewritten')
            git('branch', '-f', 'main', 'HEAD')
            with self.assertRaises(subprocess.CalledProcessError):
                setup.update(home)
            self.assertEqual(setup.load(home)['commit'], second)

    def test_run_reads_saved_ports_and_uses_shared_node_launcher(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            source = home / 'release'
            tools = {key: '/tools/' + key for key in ('git', 'go', 'node', 'python')}
            setup.atomic_json(home / 'installation.json', {'current': str(source), 'tools': tools})
            class Process:
                returncode = 0
                def poll(self):
                    return 0
                def wait(self, timeout=None):
                    return 0
            with patch.dict(os.environ, {}, clear=True), \
                    patch.object(setup, 'activate_tools'), \
                    patch.object(setup.subprocess, 'check_output', return_value='{"port":8181,"ssr-port":8282,"dev-port":8383}') as query, \
                    patch.object(setup.subprocess, 'run') as validate, \
                    patch.object(setup.subprocess, 'Popen', return_value=Process()) as launch, \
                    patch.object(setup.signal, 'signal'), \
                    patch.object(setup.threading, 'Thread'), \
                    patch('builtins.print') as output:
                with self.assertRaisesRegex(RuntimeError, 'A server exited'):
                    setup.run_servers(home)
                self.assertEqual(query.call_args.args[0][-2:], ['config', '--json'])
                self.assertEqual(validate.call_args.args[0], ['/tools/node', str(source / 'web/scripts/server-config.mjs'), 'ssr-port'])
                self.assertEqual(launch.call_args_list[1].args[0], ['/tools/node', str(source / 'web/scripts/start.mjs')])
                self.assertEqual(launch.call_args_list[0].kwargs['env']['NULAS_DATA_DIR'], str(home / 'data'))
                output.assert_called_once_with('Nulas: http://127.0.0.1:8181', flush=True)

    def test_auto_preference_and_failure_are_persisted(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            setup.atomic_json(home / 'installation.json', {'autoUpdate': False, 'current': 'old'})
            setup.set_auto(home, True)
            self.assertTrue(setup.load(home)['autoUpdate'])
            stop = setup.threading.Event()
            def fail(_):
                stop.set()
                raise RuntimeError('network unavailable')
            with patch.object(setup, 'update', side_effect=fail):
                setup.watch(home, stop)
            self.assertEqual(setup.load(home)['error'], 'network unavailable')
            self.assertEqual(setup.load(home)['current'], 'old')


if __name__ == '__main__':
    unittest.main()
