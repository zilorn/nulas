import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import threading
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
            self.assertTrue((home / '.update-lock').is_file())

    def lock_process(self, home, daemon=False):
        script = (
            "import pathlib, sys, threading, time, setup\n"
            "def hold():\n"
            "    with setup.locked(pathlib.Path(sys.argv[1])):\n"
            "        print('locked', flush=True)\n"
            "        time.sleep(60)\n"
        )
        script += ("threading.Thread(target=hold, daemon=True).start()\nsys.stdin.readline()\n"
                   if daemon else "hold()\n")
        process = subprocess.Popen([sys.executable, '-c', script, str(home)],
                                   cwd=Path(setup.__file__).parent, stdin=subprocess.PIPE,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.addCleanup(self.stop_lock_process, process)
        ready = []
        reader = threading.Thread(target=lambda: ready.append(process.stdout.readline()), daemon=True)
        reader.start()
        reader.join(timeout=10)
        self.assertEqual(ready, ['locked\n'])
        return process

    @staticmethod
    def stop_lock_process(process):
        if process.poll() is None:
            process.kill()
        process.wait(timeout=10)
        for stream in (process.stdin, process.stdout, process.stderr):
            stream.close()

    def test_lock_releases_after_killed_process(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            process = self.lock_process(home)
            with self.assertRaisesRegex(RuntimeError, 'Another installation/update'):
                with setup.locked(home):
                    self.fail('entered while another process holds the lock')
            process.kill()  # SIGKILL on POSIX, TerminateProcess on Windows
            process.wait(timeout=10)
            with setup.locked(home):
                pass

    def test_lock_releases_when_process_exits_with_daemon_updater(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            process = self.lock_process(home, daemon=True)
            process.stdin.write('stop\n')
            process.stdin.flush()
            self.assertEqual(process.wait(timeout=10), 0)
            with setup.locked(home):
                pass

    def test_lock_releases_after_exception_and_retains_same_file(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            with self.assertRaisesRegex(RuntimeError, 'build failed'):
                with setup.locked(home):
                    original = (home / '.update-lock').stat().st_ino
                    raise RuntimeError('build failed')
            with setup.locked(home):
                self.assertEqual((home / '.update-lock').stat().st_ino, original)

    def test_legacy_directory_lock_requires_explicit_recovery(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            path = home / '.update-lock'
            path.mkdir()
            with self.assertRaisesRegex(RuntimeError, 'confirm no installation/update process'):
                with setup.locked(home):
                    self.fail('cannot determine whether a legacy updater is alive')
            self.assertTrue(path.is_dir())
            path.rmdir()  # Explicit legacy recovery; later locks are OS-managed.
            with setup.locked(home):
                pass

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
                self.assertEqual(setup.load(home)["latest"], second)
                self.assertGreater(setup.load(home)["checked"], 0)
                self.assertEqual(setup.load(home)["commit"], first)
                setup.update(home)
            self.assertEqual(setup.load(home)['commit'], second)
            self.assertEqual((Path(setup.load(home)['current']) / 'file').read_text(), 'second')
            self.assertEqual((remote / 'file').read_text(), 'second')
            # Updating never removes even the failed checkout. Cleanup requires
            # the restarted current script, ready servers and a fresh latest check.
            release = Path(setup.load(home)['current'])
            old = next(item for item in (home / 'releases').iterdir() if item != release)
            protected = home / 'releases/custom-directory'
            protected.mkdir()
            (home / 'data').mkdir()
            (home / 'data/state.json').write_text('keep')
            self.assertTrue(old.exists())
            with patch.object(setup, 'servers_ready', return_value=True), \
                    patch.object(setup, 'migrate_service_directory'):
                setup.cleanup_releases(home, release, 'instance', '127.0.0.1:4669', [])
                self.assertTrue(old.exists())  # old/unrelated script cannot clean
                with patch.object(setup, '__file__', str(release / 'scripts/setup.py')):
                    with patch.object(setup, 'servers_ready', return_value=False):
                        setup.cleanup_releases(home, release, 'instance', '127.0.0.1:4669', [])
                    self.assertTrue(old.exists())
                    setup.cleanup_releases(home, release, 'instance', '127.0.0.1:4669', [])
            self.assertFalse(old.exists())
            self.assertTrue(release.exists())
            self.assertTrue(protected.exists())
            self.assertEqual((home / 'data/state.json').read_text(), 'keep')
            # A force-pushed/divergent history must never become active.
            git('checkout', '--orphan', 'rewrite')
            git('commit', '-am', 'rewritten')
            git('branch', '-f', 'main', 'HEAD')
            with self.assertRaises(subprocess.CalledProcessError):
                setup.update(home)
            self.assertEqual(setup.load(home)['commit'], second)

    def test_cleanup_guards_and_retries_failed_deletion(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            current = home / 'releases' / ('a' * 12 + '-2')
            old = home / 'releases' / ('b' * 12 + '-1')
            for release in (old, current):
                (release / '.git').mkdir(parents=True)
            outside = home / 'outside'
            outside.mkdir()
            (home / 'releases' / ('c' * 12 + '-3')).symlink_to(outside, target_is_directory=True)
            metadata = {'current': str(current), 'commit': 'a' * 40, 'latest': 'a' * 40, 'branch': 'main'}
            setup.atomic_json(home / 'installation.json', metadata)
            latest = 'a' * 40
            def git(*args, **kwargs):
                if args[1:3] == ('rev-parse', 'FETCH_HEAD'):
                    return latest
                if args[1:3] == ('rev-parse', 'HEAD'):
                    return ('b' if kwargs['cwd'] == old else 'a') * 40
                return ''
            with patch.object(setup, '__file__', str(current / 'scripts/setup.py')), \
                    patch.object(setup, 'servers_ready', return_value=True), \
                    patch.object(setup, 'command', side_effect=git), \
                    patch.object(setup, 'migrate_service_directory') as migrate:
                latest = 'd' * 40
                setup.cleanup_releases(home, current, 'id', '127.0.0.1:4669', [])
                self.assertTrue(old.exists())
                migrate.assert_not_called()
                latest = 'a' * 40
                setup.atomic_json(home / 'installation.json', metadata)
                with patch.object(setup, 'migrate_service_directory', side_effect=RuntimeError('reload failed')):
                    with self.assertRaisesRegex(RuntimeError, 'reload failed'):
                        setup.cleanup_releases(home, current, 'id', '127.0.0.1:4669', [])
                self.assertTrue(old.exists())
                with patch.object(setup.shutil, 'rmtree', side_effect=OSError('in use')):
                    setup.cleanup_releases(home, current, 'id', '127.0.0.1:4669', [])
                self.assertTrue(old.exists())
                setup.cleanup_releases(home, current, 'id', '127.0.0.1:4669', [])
                self.assertFalse(old.exists())
                self.assertTrue(current.exists())
                self.assertTrue(outside.exists())

    def test_readiness_rejects_other_instance_and_dead_server(self):
        from unittest.mock import MagicMock
        opener = MagicMock()
        response = opener.open.return_value.__enter__.return_value
        response.read.return_value = b'{"status":"ok","instance":"old"}'
        with patch.object(setup.urllib.request, 'build_opener', return_value=opener):
            self.assertFalse(setup.servers_ready('127.0.0.1:4669', 'new', []))
            self.assertEqual(opener.open.call_count, 1)
            response.read.return_value = b'{"status":"ok","instance":"new"}'
            response.status = 200
            response.headers = {'Content-Type': 'text/html'}
            process = MagicMock()
            process.poll.return_value = 1
            self.assertFalse(setup.servers_ready('127.0.0.1:4669', 'new', [process]))
            process.poll.return_value = None
            self.assertTrue(setup.servers_ready('[::1]:4669', 'new', [process]))
            self.assertFalse(setup.servers_ready('192.0.2.1:4669', 'new', [process]))

    def test_startup_cleans_only_after_ready_and_before_update_watcher(self):
        from unittest.mock import MagicMock
        with tempfile.TemporaryDirectory() as temporary:
            home = Path(temporary)
            source = home / 'releases/current'
            setup.atomic_json(home / 'installation.json', {'current': str(source),
                'tools': {key: '/tools/' + key for key in ('git', 'go', 'node', 'python')}})
            process = MagicMock()
            process.poll.return_value = None
            stop = MagicMock()
            stop.wait.side_effect = [False, False, False, True]
            events = []
            def ready(*args):
                events.append('ready')
                return len(events) > 1
            def cleanup(*args):
                events.append('cleanup')
            with patch.dict(os.environ, {}, clear=True), \
                    patch.object(setup, 'activate_tools'), \
                    patch.object(setup.subprocess, 'check_output', return_value='{"port":4669}'), \
                    patch.object(setup.subprocess, 'run'), \
                    patch.object(setup.subprocess, 'Popen', return_value=process) as launch, \
                    patch.object(setup.signal, 'signal'), \
                    patch.object(setup.threading, 'Event', return_value=stop), \
                    patch.object(setup.threading, 'Thread') as watcher, \
                    patch.object(setup, 'servers_ready', side_effect=ready), \
                    patch.object(setup, 'cleanup_releases', side_effect=cleanup) as clean:
                watcher.return_value.start.side_effect = lambda: events.append('watch')
                setup.run_servers(home)
                self.assertEqual(events, ['ready', 'ready', 'ready', 'cleanup', 'watch'])
                token = launch.call_args_list[0].kwargs['env']['NULAS_RUN_INSTANCE']
                self.assertEqual(clean.call_args.args[2], token)
                self.assertEqual(len(token), 64)

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

    def removal_fixture(self, home):
        release = home / 'releases/current'
        release.mkdir(parents=True)
        for name in ('source', 'tools', 'bin', 'data', 'runtime'):
            (home / name).mkdir()
            (home / name / 'keep').write_text(name)
        setup.atomic_json(home / 'installation.json', {'current': str(release)})
        return release

    def test_remove_preserves_data_runtime_and_unrelated_shell_settings(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {}, clear=True):
            user = Path(directory)
            home = user / 'nulas'
            self.removal_fixture(home)
            (home / 'custom-file').write_text('keep')
            (user / '.bashrc').write_text('alias keep=true\n')
            setup.add_path(home, user)
            real_add_path = setup.add_path
            with patch.object(setup, 'add_path', side_effect=lambda h, remove: real_add_path(h, user, remove)), \
                    patch('install_service.uninstall_service') as uninstall:
                setup.remove(home)
                if sys.platform == 'linux':
                    uninstall.assert_called_once_with(home)
            self.assertEqual((user / '.bashrc').read_text(), 'alias keep=true\n')
            self.assertFalse((user / '.config/fish/conf.d/nulas.fish').exists())
            for name in ('data', 'runtime'):
                self.assertEqual((home / name / 'keep').read_text(), name)
            self.assertEqual((home / 'custom-file').read_text(), 'keep')
            for name in ('source', 'releases', 'tools', 'bin', 'installation.json'):
                self.assertFalse((home / name).exists())

    def test_remove_rejects_invalid_paths_locks_and_stop_failures(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory) / 'nulas'
            release = self.removal_fixture(home)
            with patch.object(setup, 'add_path') as paths, patch('install_service.uninstall_service') as uninstall:
                setup.atomic_json(home / 'installation.json', {'current': str(home.parent)})
                with self.assertRaisesRegex(RuntimeError, 'Current release'):
                    setup.remove(home)
                setup.atomic_json(home / 'installation.json', {'current': str(release)})
                with setup.locked(home):
                    with self.assertRaisesRegex(RuntimeError, 'Another installation/update'):
                        setup.remove(home)
                (home / '.update-lock').unlink()
                (home / '.update-lock').mkdir()
                with self.assertRaisesRegex(RuntimeError, 'Legacy installation/update'):
                    setup.remove(home)
                (home / '.update-lock').rmdir()
                (home / '.git').mkdir()
                with self.assertRaisesRegex(RuntimeError, 'development checkout'):
                    setup.remove(home)
                (home / '.git').rmdir()
                if sys.platform == 'linux':
                    uninstall.side_effect = RuntimeError('stop failed')
                    with self.assertRaisesRegex(RuntimeError, 'stop failed'):
                        setup.remove(home)
                    self.assertTrue((home / 'installation.json').exists())
                    self.assertTrue(release.exists())
                paths.assert_not_called()

    def test_remove_rejects_symlinks_and_data_overrides_inside_software(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory) / 'nulas'
            release = self.removal_fixture(home)
            outside = Path(directory) / 'outside'
            outside.mkdir()
            (home / 'tools/keep').unlink()
            (home / 'tools').rmdir()
            (home / 'tools').symlink_to(outside, target_is_directory=True)
            with patch.object(setup, 'add_path') as paths, patch('install_service.uninstall_service') as uninstall:
                with self.assertRaisesRegex(RuntimeError, 'unexpected installation path'):
                    setup.remove(home)
                (home / 'tools').unlink()
                for key in ('NULAS_DATA_DIR', 'NULAS_CORE_DIR'):
                    with patch.dict(os.environ, {key: str(release / 'data')}):
                        with self.assertRaisesRegex(RuntimeError, key):
                            setup.remove(home)
                paths.assert_not_called()
                uninstall.assert_not_called()
                self.assertTrue(release.exists())
                self.assertTrue(outside.exists())

    def test_remove_deletion_failure_retains_metadata_and_reports_error(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory) / 'nulas'
            self.removal_fixture(home)
            with patch.object(setup, 'add_path'), patch('install_service.uninstall_service'), \
                    patch.object(setup.shutil, 'rmtree', side_effect=OSError('file in use')), \
                    patch('builtins.print') as output:
                with self.assertRaisesRegex(OSError, 'file in use'):
                    setup.remove(home)
                self.assertTrue((home / 'installation.json').exists())
                self.assertTrue((home / '.update-lock').is_file())
                output.assert_not_called()

    def test_launcher_remove_bypasses_running_binary(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            release = home / 'releases/current'
            (release / 'scripts').mkdir(parents=True)
            (release / 'scripts/setup.py').write_text('import sys; print(sys.argv[1:])')
            setup.atomic_json(home / 'installation.json', {'current': str(release),
                'tools': {key: sys.executable for key in ('git', 'go', 'node', 'python')}})
            setup.launchers(home)
            output = subprocess.check_output([sys.executable, str(home / 'bin/launcher.py'), 'remove'], text=True)
            self.assertIn("'remove', '--home', " + repr(str(home)), output)


if __name__ == '__main__':
    unittest.main()
