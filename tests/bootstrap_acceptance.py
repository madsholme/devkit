#!/usr/bin/env python3
"""Run bootstrap distro branches with mocked host commands; never invokes real sudo."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "bootstrap.sh"
MOCK = r'''#!/usr/bin/env python3
import json, os, pathlib, sys
name=pathlib.Path(sys.argv[0]).name
args=sys.argv[1:]
root=pathlib.Path(os.environ['MOCK_ROOT'])
if name=='sudo':
 name,args=args[0],args[1:]
with (root/'calls').open('a') as f: f.write(json.dumps([name,*args])+'\n')
if name=='id':
 if args[0]=='-u': print('1000')
 elif args[0]=='-un': print('demo')
 elif args[0]=='-nG': print((root/'groups').read_text())
 else: sys.exit(90)
elif name=='uname': print(os.environ.get('MOCK_ARCH','x86_64'))
elif name=='getent': print('group:x:1000:')
elif name=='usermod':
 assert args[0]=='-aG' and args[2]=='demo'
 with (root/'groups').open('a') as f: f.write(' '+args[1])
elif name=='systemctl':
 if args[0]=='is-active': sys.exit(0 if os.environ.get('MOCK_ACTIVE')=='yes' else 1)
 if args[0]=='cat': sys.exit(0 if os.environ.get('MOCK_MODULAR')=='yes' else 1)
elif name in ('apt-get','dnf'):
 if os.environ.get('MOCK_FAIL')=='yes' and 'swtpm' in args: sys.exit(42)
elif name=='docker': print('Docker mock')
elif name=='tailscale': print('100.64.0.1' if args[0]=='ip' else 'mock')
elif name=='loginctl': print('yes')
elif name=='git': print('demo@example.test')
elif name in ('firewall-cmd','ufw'): sys.exit(1)
else: sys.exit(90)
'''

class Bootstrap(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        mock = self.bin / 'mock'
        mock.write_text(MOCK)
        mock.chmod(0o755)
        for name in ('sudo', 'id', 'uname', 'getent', 'usermod', 'systemctl',
                     'apt-get', 'dnf', 'docker', 'tailscale', 'loginctl', 'git',
                     'firewall-cmd', 'ufw'):
            (self.bin / name).symlink_to(mock)
        (self.root / 'groups').write_text('demo docker')
        # Substitute OS discovery only; execute the real installer logic unchanged.
        script = SCRIPT.read_text().replace('/etc/os-release', str(self.root / 'os-release'))
        script = script.replace('/run/systemd/system', str(self.root))
        self.script = self.root / 'bootstrap.sh'
        self.script.write_text(script)

    def run_bootstrap(self, distro='ubuntu', args=('--qemu',), **overrides):
        (self.root / 'os-release').write_text(f'ID={distro}\nVERSION_CODENAME=noble\n')
        env = dict(os.environ, PATH=str(self.bin)+':/usr/bin:/bin', MOCK_ROOT=str(self.root), **overrides)
        result = subprocess.run(['sh', str(self.script), *args], env=env, capture_output=True, text=True, timeout=10)
        calls = [json.loads(s) for s in (self.root / 'calls').read_text().splitlines()] if (self.root/'calls').exists() else []
        return result, calls

    def test_ubuntu_and_idempotent_groups(self):
        result, calls = self.run_bootstrap()
        self.assertEqual(result.returncode, 0, result.stderr)
        packages = next(c for c in calls if c[0]=='apt-get' and 'swtpm' in c)
        self.assertIn('qemu-system-x86', packages)
        self.assertIn('ovmf', packages)
        self.assertIn(['systemctl','enable','--now','libvirtd.service'], calls)
        self.assertEqual(sum(c[0]=='usermod' for c in calls), 2)
        result, calls = self.run_bootstrap()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sum(c[0]=='usermod' for c in calls), 2)

    def test_fedora_modular_and_spice(self):
        result, calls = self.run_bootstrap('fedora', MOCK_MODULAR='yes')
        self.assertEqual(result.returncode, 0, result.stderr)
        packages = next(c for c in calls if c[0]=='dnf' and 'swtpm' in c)
        for package in ('qemu-ui-spice-core','qemu-device-display-qxl','edk2-ovmf','libvirt-daemon-kvm'):
            self.assertIn(package, packages)
        self.assertIn(['systemctl','enable','--now','virtqemud.socket','virtqemud-ro.socket'], calls)

    def test_existing_daemon_is_preserved(self):
        result, calls = self.run_bootstrap('fedora', MOCK_MODULAR='yes', MOCK_ACTIVE='yes')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any(c[0]=='systemctl' and 'enable' in c for c in calls))

    def test_arm_and_normal_bootstrap(self):
        result, calls = self.run_bootstrap(MOCK_ARCH='aarch64')
        self.assertEqual(result.returncode, 0, result.stderr)
        packages = next(c for c in calls if c[0]=='apt-get' and 'swtpm' in c)
        self.assertIn('qemu-system-arm', packages)
        self.assertIn('qemu-efi-aarch64', packages)
        (self.root/'calls').unlink()
        result, calls = self.run_bootstrap(args=())
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any('swtpm' in c or 'libvirt' in c for c in calls))

    def test_failure_stops_before_groups_and_services(self):
        result, calls = self.run_bootstrap(MOCK_FAIL='yes')
        self.assertEqual(result.returncode, 42)
        self.assertFalse(any(c[0] in ('usermod','systemctl') for c in calls))

    def test_unknown_option_has_no_side_effects(self):
        result, calls = self.run_bootstrap(args=('--typo',))
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(calls, [])

if __name__ == '__main__':
    unittest.main()
