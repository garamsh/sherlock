"""Issue 840 study harness: built product loop, isolated CLI workspace, metered model.

Run from the repository root: python3 docs/research/repository-cli/qa.py preflight|live|comparison-preflight|comparison-live
Requires make build-images at the checked-out revision and Docker Desktop host access.
The live mode is single-use; its directory must not already exist. Never retry it.
"""
import http.server
import json
import io
import tarfile
import os
from pathlib import Path
import secrets
import signal
import socket
import ssl
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request

REPO = Path(__file__).resolve().parents[3]
COMPARISON = sys.argv[1].startswith('comparison-')
ROOT = REPO / ('.local/verification/github-840-comparison' if COMPARISON else '.local/verification/github-840-cli')
MODE = sys.argv[1].removeprefix('comparison-')
assert MODE in ('preflight', 'live')
OUT = ROOT / MODE
OUT.mkdir(mode=0o700)
os.umask(0o077)
DOCKER_HOST = subprocess.check_output(['docker', 'context', 'inspect', '--format', '{{.Endpoints.docker.Host}}'], text=True).strip()
BASE = {'DOCKER_HOST': DOCKER_HOST, 'PATH': '/usr/bin:/bin', 'HOME': '/nonexistent', 'LANG': 'C.UTF-8'}
LIMIT = {'calls': 40, 'input': 150000, 'output': 20000, 'usd': 0.10}
BRANCH = 'docs/gagent-cli-840-20260925'
REVISION = subprocess.check_output(['git', 'rev-parse', 'HEAD'], env=BASE, cwd=REPO, text=True).strip()
TASK = '''Complete one bounded repository task on garamsh/agent-test using ordinary git and gh through shell_exec and the installed file tools. Clone the repository into /workspace/repo, inspect its actual starting revision and applicable contributor instructions, and independently choose ONE small substantive permitted documentation improvement grounded in the repository. Respect all ownership and contributor restrictions; this task grants no ownership override or bootstrap authority. If no permitted change can be made, report that accurately and stop without mutation. Otherwise create the fresh non-default branch docs/gagent-cli-840-20260925, make your chosen improvement, validate offline with sh /validator.sh from the checkout plus any appropriate specific checks, inspect your diff, commit, push only that branch, and use gh to open a DRAFT pull request describing the change, actual checks and limitations. Never push the default branch, merge, create issues/comments, change conventions, or mutate another repository. Do not print credentials or environment contents; the workspace deliberately has repository credentials and networking, but no model keys or agent memory. You have at most six calls per tool in this turn: batch related shell operations and use file_read/file_write where useful; this is a normal runtime limit, not permission to skip instruction reads or validation. GITHUB_REPOSITORY=garamsh/agent-test and GITHUB_BRANCH=docs/gagent-cli-840-20260925 are operator-provided. For HTTPS authentication, gh auth setup-git configures normal git credential helpers; do not embed credentials in URLs or files. Report starting revision, selected change, exact checks and their results, branch and draft PR URL, or the concrete blocker. No paid retry or task broadening is available.'''
if COMPARISON:
    new_branch = 'docs/gagent-cli-840-comparison-20260925'
    TASK = TASK.replace(BRANCH, new_branch)
    BRANCH = new_branch
    TASK = TASK.replace('You have at most six calls per tool in this turn: batch related shell operations and use file_read/file_write where useful; this is a normal runtime limit, not permission to skip instruction reads or validation.', 'The whole turn has a 16-iteration ceiling; identical failing calls are suppressed.')
(OUT / 'task.json').write_text(json.dumps({'task': TASK, 'revision': REVISION, 'limits': LIMIT, 'model': 'MiniMax-M2', 'rates_per_million_usd': [0.3, 1.2], 'rate_source': 'https://www.minimax.io/news/minimax-m2', 'branch': BRANCH, 'repository': 'garamsh/agent-test'}, indent=2))
(OUT / 'workspace').mkdir(mode=0o777)
(OUT / 'workspace').chmod(0o777)
(OUT / 'agent').mkdir()
(OUT / 'tools').mkdir()
tool_container = 'gagent-cli-840-tool-copy'
subprocess.run(['docker', 'create', '--name', tool_container, 'gagent-tools:'+REVISION, '/unused'], env=BASE, stdout=subprocess.DEVNULL, check=True)
try:
    for tool_name in ('file_read', 'file_write', 'file_list', 'shell_exec', 'message_send'):
        archive = subprocess.check_output(['docker', 'cp', tool_container+':/'+tool_name, '-'], env=BASE)
        (OUT/'tools'/tool_name).mkdir()
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            for member in tar.getmembers():
                if member.isfile():
                    target = OUT/'tools'/tool_name/Path(member.name).name
                    target.write_bytes(tar.extractfile(member).read())
                    target.chmod(0o755 if target.name == tool_name else 0o644)
finally:
    subprocess.run(['docker', 'rm', tool_container], env=BASE, stdout=subprocess.DEVNULL, check=True)
(OUT / 'validator.sh').write_text('''#!/bin/sh
set -eu
git diff --check
if git grep -n -E '^(<<<<<<< |=======|>>>>>>> )' -- '*.md'; then
  echo 'FAIL conflict markers'; exit 1
fi
echo 'PASS offline diff whitespace and Markdown conflict-marker checks'
''')
(OUT / 'validator.sh').chmod(0o444)
(OUT / 'empty.yaml').write_text('{}\n')
(OUT / 'empty.yaml').chmod(0o444)


def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]


credentials = {}
if MODE == 'live':
    for line in Path('/home/sauce/Workspace/garam/gagent/.env').read_text().splitlines():
        name, sep, value = line.partition('=')
        if sep and name.strip() in ('GH_TOKEN', 'MINIMAX_KEY'):
            credentials[name.strip()] = value.strip().strip('"').strip("'")
    assert all(credentials.get(k) for k in ('GH_TOKEN', 'MINIMAX_KEY'))
MODEL_AUTH = secrets.token_urlsafe(32)
private_values = list(credentials.values()) + [MODEL_AUTH]


def valid_usage(usage):
    return isinstance(usage, dict) and all(type(usage.get(k)) is int and usage[k] >= 0 for k in ('prompt_tokens', 'completion_tokens'))


def allowed(calls, inputs, outputs, reserve, maximum, stopped):
    return not stopped and calls < LIMIT['calls'] and maximum > 0 and inputs + reserve <= LIMIT['input'] and outputs + maximum <= LIMIT['output'] and ((inputs + reserve) * .3 + (outputs + maximum) * 1.2) / 1e6 <= LIMIT['usd']


assert valid_usage({'prompt_tokens': 1, 'completion_tokens': 0})
assert all(not valid_usage(u) for u in ({}, {'prompt_tokens': True, 'completion_tokens': 1}, {'prompt_tokens': -1, 'completion_tokens': 1}, {'prompt_tokens': 1, 'completion_tokens': '1'}))
assert allowed(0, 0, 0, 4096, 3072, False)
assert not allowed(40, 0, 0, 4096, 3072, False)
assert not allowed(0, 149000, 0, 4096, 3072, False)
assert not allowed(0, 0, 19999, 4096, 3072, False)
assert not allowed(0, 0, 0, 4096, 3072, True)
(OUT/'budget-controls.json').write_text(json.dumps({'accepted_usage_and_reservation': True, 'refused_missing_boolean_negative_string_usage': True, 'refused_call_input_output_and_stopped_ceiling': True}))


def meter_snapshot(meter_class):
    return {'calls': meter_class.calls, 'input_tokens': meter_class.inputs,
            'output_tokens': meter_class.outputs,
            'usd': (meter_class.inputs * .3 + meter_class.outputs * 1.2) / 1e6,
            'stopped': meter_class.stopped, 'limits': LIMIT}


class Meter(http.server.BaseHTTPRequestHandler):
    calls = inputs = outputs = step = 0
    stopped = False

    def log_message(self, *args):
        pass

    def do_GET(self):
        (OUT/'meter-network-refusal.json').write_text(json.dumps({'path': self.path, 'status': 403}))
        self.send_response(403)
        self.end_headers()
        self.wfile.write(b'agent authentication required')

    def do_POST(self):
        cls = type(self)
        if self.headers.get('Authorization') != 'Bearer ' + MODEL_AUTH:
            self.send_response(403)
            self.end_headers()
            self.wfile.write(b'agent authentication required')
            return
        data = self.rfile.read(int(self.headers['Content-Length']))
        assert not any(v.encode() in data for v in private_values)
        req = json.loads(data)
        if MODE == 'live':
            reserve = len(data) + 4096
            maximum = min(3072, LIMIT['output'] - cls.outputs)
            if not allowed(cls.calls, cls.inputs, cls.outputs, reserve, maximum, cls.stopped):
                cls.stopped = True
                (OUT/'refused-request.json').write_bytes(data)
                self.send_response(402)
                self.end_headers()
                self.wfile.write(b'{"error":"hard budget reservation refused"}')
                return
            req['max_tokens'] = maximum
            data = json.dumps(req).encode()
            cls.calls += 1
            cls.stopped = True  # Fail closed until a complete accounted response arrives.
            (OUT / f'request-{cls.calls:02d}.json').write_bytes(data)
            request = urllib.request.Request('https://api.minimax.io/v1/chat/completions', data, {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + credentials['MINIMAX_KEY']})
            try:
                with urllib.request.urlopen(request, timeout=240) as r:
                    result, status = r.read(), r.status
            except urllib.error.HTTPError as e:
                result, status = e.read(), e.code
            except Exception:
                result, status = b'{"error":"upstream transport failed; stopped"}', 502
                cls.stopped = True
            assert not any(v.encode() in result for v in private_values)
            (OUT / f'response-{cls.calls:02d}.json').write_bytes(result)
            try:
                usage = json.loads(result).get('usage', {})
            except (ValueError, AttributeError):
                usage = {}
            if not valid_usage(usage):
                cls.inputs += reserve
                cls.outputs += maximum
                cls.stopped = True
            else:
                cls.inputs += usage['prompt_tokens']
                cls.outputs += usage['completion_tokens']
                cls.stopped = status != 200 or usage['prompt_tokens'] > reserve or usage['completion_tokens'] > maximum
        else:
            if [t['function']['name'] for t in req.get('tools', [])] == ['memory_search_terms']:
                name, args = 'memory_search_terms', {'phrases': ['local CLI fixture']}
            else:
                name, args = STEPS[min(cls.step, len(STEPS)-1)]
                cls.step += 1
            result = json.dumps({'choices': [{'message': {'content': '', 'tool_calls': [{'id': f'call{cls.step}', 'type': 'function', 'function': {'name': name, 'arguments': json.dumps(args)}}]}, 'finish_reason': 'tool_calls'}], 'usage': {'prompt_tokens': 100, 'completion_tokens': 20}}).encode()
            status = 200
            cls.calls += 1
            (OUT / f'request-{cls.calls:02d}.json').write_bytes(data)
            (OUT / f'response-{cls.calls:02d}.json').write_bytes(result)
        (OUT / 'meter.json').write_text(json.dumps(meter_snapshot(cls), indent=2))
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(result)


meter = http.server.HTTPServer(('0.0.0.0', 0), Meter)
threading.Thread(target=meter.serve_forever, daemon=True).start()
# An unauthenticated reachable meter is not model authority. This host-side
# control tests the same endpoint the network-enabled workspace can reach.
try:
    urllib.request.urlopen(urllib.request.Request(f'http://127.0.0.1:{meter.server_port}', b'{}'), timeout=5)
    raise AssertionError('meter accepted missing authentication')
except urllib.error.HTTPError as e:
    assert e.code == 403
(OUT / 'meter-auth.json').write_text(json.dumps({'unauthenticated_control': 403, 'accepted_control': 'actual product-agent authenticated requests in request/response evidence'}))

fixture = None
if MODE == 'preflight':
    subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', str(OUT/'tls.key'), '-out', str(OUT/'tls.crt'), '-days', '1', '-subj', '/CN=host.docker.internal', '-addext', 'subjectAltName=DNS:host.docker.internal'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, env=BASE)
    (OUT/'tls.crt').chmod(0o444)

    class GitHubFixture(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
            accepted = self.path == '/api/v3/repos/fixture/repo/pulls' and body.get('draft') is True and body.get('head') == 'qa-fixture' and self.headers.get('Authorization') == 'token fixture-token'
            (OUT/'fixture-request.json').write_text(json.dumps({'path': self.path, 'body': body, 'marker_auth_matches': self.headers.get('Authorization') == 'token fixture-token'}, indent=2))
            self.send_response(201 if accepted else 400)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps({'html_url': 'https://fixture.invalid/pull/1', 'draft': True} if accepted else {'message': 'fixture refused'}).encode())

    fixture = http.server.HTTPServer(('0.0.0.0', 0), GitHubFixture)
    tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    tls.load_cert_chain(OUT/'tls.crt', OUT/'tls.key')
    fixture.socket = tls.wrap_socket(fixture.socket, server_side=True)
    threading.Thread(target=fixture.serve_forever, daemon=True).start()
    STEPS = [
        ('shell_exec', {'command': f'''set -eu
[ "$GH_TOKEN" = repository-marker ]; [ "${{MINIMAX_KEY-unset}}" = unset ]; [ "${{QA_AGENT_ONLY-unset}}" = unset ]; [ "${{QA_MODEL_AUTH-unset}}" = unset ]
[ ! -e /home/sauce ]; [ ! -e /agent ]; [ ! -e /var/run/docker.sock ]
[ "$(id -u)" = 10001 ]; git --version; gh --version
if git ls-remote http://host.docker.internal:{meter.server_port}/model-control; then exit 1; fi
echo 'PASS isolated environment, filesystem, UID, CLI availability; model access refused'
git init --bare /workspace/remote.git
git clone /workspace/remote.git /workspace/repo
cd /workspace/repo; git config user.name 'Gagent QA'; git config user.email 'qa@example.invalid'
printf '# Fixture\\n\\nInitial content.\\n' > README.md
git add README.md; git commit -m 'docs: initialize fixture'; git push origin HEAD:main
git switch -c qa-fixture'''}),
        ('file_read', {'path': 'repo/README.md'}),
        ('file_write', {'path': 'repo/README.md', 'content': '# Fixture\n\nInitial content.\n\nRun `sh /validator.sh` before publishing a change.\n'}),
        ('shell_exec', {'command': 'set -eu; cd /workspace/repo; sh /validator.sh; git diff; git add README.md; git commit -m "docs: explain fixture validation"; git push origin qa-fixture'}),
        ('shell_exec', {'command': f'''gh api --hostname host.docker.internal https://host.docker.internal:{fixture.server_port}/api/v3/repos/fixture/repo/pulls -f title='Document validation' -f head=qa-fixture -f base=main -F draft=true -f body='Validated with sh /validator.sh' '''}),
        ('shell_exec', {'command': 'git -C /workspace/repo rev-parse --verify missing-ref'}),
        ('shell_exec', {'command': 'sleep 2', 'timeout_nanos': 100000000}),
        ('shell_exec', {'command': "cd /workspace/repo; git status --porcelain; git ls-remote origin refs/heads/qa-fixture; printf 'resource caps\\n'; cat /proc/self/limits"}),
        ('memory_write', {'reply': 'Deterministic CLI fixture completed; inspect trace for success, nonzero command and timeout controls.', 'memory_ops': []}),
    ]

if COMPARISON and MODE == 'preflight':
    STEPS[-1:-1] = [
        ('shell_exec', {'command': "git -C /workspace/repo status --porcelain; printf 'seventh shell call executed\\n'"}),
        ('shell_exec', {'command': "git -C /workspace/repo log -1 --format=%s; printf 'eighth shell call executed\\n'"}),
    ]

wp = port()
name = ('gagent-cli-840-comparison-' if COMPARISON else 'gagent-cli-840-') + MODE
workspace_env = BASE | {'GH_TOKEN': credentials.get('GH_TOKEN', 'repository-marker'), 'GITHUB_REPOSITORY': 'garamsh/agent-test', 'GITHUB_BRANCH': BRANCH, 'GIT_AUTHOR_NAME': 'Gagent QA', 'GIT_AUTHOR_EMAIL': 'gagent-qa@users.noreply.github.com', 'GIT_COMMITTER_NAME': 'Gagent QA', 'GIT_COMMITTER_EMAIL': 'gagent-qa@users.noreply.github.com'}
keys = [k for k in workspace_env if k not in BASE]
if MODE == 'preflight':
    workspace_env |= {'GH_ENTERPRISE_TOKEN': 'fixture-token', 'SSL_CERT_FILE': '/fixture.crt'}
    keys += ['GH_ENTERPRISE_TOKEN', 'SSL_CERT_FILE']
cmd = ['docker', 'run', '-d', '--name', name, '--cap-drop', 'ALL']
for cap in ('SETUID', 'SETGID', 'CHOWN', 'SYS_ADMIN', 'KILL'):
    cmd += ['--cap-add', cap]
for key in keys:
    cmd += ['-e', key]
cmd += ['-v', f'{OUT}/workspace:/workspace', '-v', f'{OUT}/validator.sh:/validator.sh:ro']
if MODE == 'preflight':
    cmd += ['-v', f'{OUT}/tls.crt:/fixture.crt:ro']
cmd += ['gagent-workspace:' + REVISION, 'workspace', '--addr', '127.0.0.1:8081', '--workspace', '/workspace', '--exec-uid', '10001', '--exec-network', '--exec-env', ','.join(keys), '--exec-timeout', '90s']
(OUT/'workspace-argv.json').write_text(json.dumps(cmd, indent=2))
(OUT/'environment-keys.json').write_text(json.dumps({'workspace': sorted(workspace_env), 'agent': sorted(BASE | {'GAGENT_TOOLS_DIR':'', 'QA_MODEL_AUTH':'', 'QA_AGENT_ONLY':''}), 'model_access_only': ['MINIMAX_KEY'], 'workspace_grants': keys}, indent=2))
processes = []
started = False
try:
    subprocess.run(cmd, env=workspace_env, stdout=subprocess.DEVNULL, check=True)
    started = True
    time.sleep(.2)
    running = subprocess.check_output(['docker', 'inspect', '--format', '{{.State.Running}}', name], env=BASE, text=True).strip()
    assert running == 'true', 'workspace failed startup'
    config = OUT/'agent.yaml'
    config.write_text(f'model:\n  provider: openai-compatible\n  base-url: http://host.docker.internal:{meter.server_port}\n  model: MiniMax-M2\n  api-key-env: QA_MODEL_AUTH\n  timeout: 300s\n')
    agentenv = BASE | {'QA_MODEL_AUTH': MODEL_AUTH, 'QA_AGENT_ONLY': 'agent-only-marker'}
    command = ['docker', 'run', '--rm', '--name', name+'-agent', '--network', 'container:'+name, '--user', str(os.getuid()), '--cap-drop', 'ALL', '-e', 'QA_MODEL_AUTH', '-e', 'QA_AGENT_ONLY', '-e', 'GAGENT_TOOLS_DIR=/tools', '-v', f'{OUT}/tools:/tools:ro', '-v', f'{OUT}/agent:/agent', '-v', f'{config}:/config.yaml:ro', 'gagent-agent:'+REVISION, 'agent', '--config', '/config.yaml', '--agent-id', 'repository-cli-qa', '--memory-path', '/agent/memory.db', '--workspace-addr', '127.0.0.1:8081', '--message', TASK if MODE == 'live' else 'Run the deterministic local CLI fixture.']
    with open(OUT/'agent.log', 'w') as log:
        agent = subprocess.Popen(command, env=agentenv, cwd=OUT/'agent', stdout=log, stderr=log, start_new_session=True)
        processes.append(agent)
        rc = agent.wait(timeout=1200)
    (OUT/'agent-exit.txt').write_text(str(rc))
finally:
    for p in processes:
        if p.poll() is None:
            os.killpg(p.pid, signal.SIGTERM)
            try:
                p.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(p.pid, signal.SIGKILL)
                p.wait()
    subprocess.run(['docker', 'rm', '-f', name+'-agent'], env=BASE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    if started:
        with open(OUT/'workspace.log', 'w') as log:
            subprocess.run(['docker', 'logs', name], env=BASE, stdout=log, stderr=log)
        subprocess.run(['docker', 'rm', '-f', name], env=BASE, stdout=subprocess.DEVNULL, check=True)
    meter.shutdown()
    meter.server_close()
    (OUT/'meter-final.json').write_text(json.dumps(meter_snapshot(Meter), indent=2))
    if fixture:
        fixture.shutdown()
        fixture.server_close()
    matches = []
    scanned = 0
    for p in OUT.rglob('*'):
        if p.is_file() and 'tools' not in p.relative_to(OUT).parts:
            scanned += 1
            if any(v.encode() in p.read_bytes() for v in private_values):
                matches.append(str(p.relative_to(OUT)))
    checkout = OUT/'workspace/repo'
    object_scan = False
    if (checkout/'.git').is_dir():
        objects = subprocess.run(['git', '-c', 'safe.directory='+str(checkout), '-C', str(checkout), 'cat-file', '--batch-all-objects', '--batch'], env=BASE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True).stdout
        object_scan = True
        if any(v.encode() in objects for v in private_values):
            matches.append('git-object-content')
    (OUT/'secret-scan.json').write_text(json.dumps({'exact_secret_match_files': matches, 'files_scanned': scanned, 'git_object_content_scanned': object_scan, 'processes_exited': all(p.poll() is not None for p in processes), 'container_removed': started}, indent=2))
    assert not matches
print(json.dumps({'mode': MODE, 'calls': Meter.calls, 'input': Meter.inputs, 'output': Meter.outputs, 'stopped': Meter.stopped, 'processes_exited': True}))
