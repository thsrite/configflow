"""Write a sample of Python-rendered installer scripts for the shellcheck comparison (A08 review).

Every 4th rendered shell case of matrix.jsonl (go_agent, aio, gateway, core, run, with_install_output)
and every 40th of fuzz.jsonl.gz is rendered with the Python generators into DIR/py/<source>-<id>.sh;
TestWriteShellcheckSample (INSTALLER_SAMPLE_DIR=DIR) writes the Go renderings to DIR/go/. Then:

    sudo docker run --rm -v DIR:/w koalaman/shellcheck:stable -f gcc /w/py/*.sh  (and /w/go/*.sh)
"""
import gzip
import json
import os
import sys

sys.path.insert(0, '.')
from backend.agents import gateway_install_script as gateway  # noqa: E402
from backend.agents.core_install_script import generate_core_install_script  # noqa: E402
from backend.agents.go_install_script import generate_go_agent_install_script, generate_go_aio_install_script  # noqa: E402
from backend.agents.install_output import with_install_output  # noqa: E402
from backend.agents.install_script import generate_docker_agent_run  # noqa: E402

FUNCTIONS = {'go_agent': generate_go_agent_install_script, 'aio': generate_go_aio_install_script,
             'core': generate_core_install_script, 'gateway': gateway.generate_gateway_install_script,
             'run': generate_docker_agent_run, 'with_install_output': with_install_output}

SHELL = {'go_agent', 'aio', 'gateway', 'core', 'run', 'with_install_output'}
directory = os.path.join(sys.argv[1], 'py')
os.makedirs(directory, exist_ok=True)
here = os.path.dirname(__file__)
sources = [('matrix', open(os.path.join(here, 'matrix.jsonl'), encoding='utf-8'), 4),
           ('fuzz', gzip.open(os.path.join(here, 'fuzz.jsonl.gz'), 'rt', encoding='utf-8'), 40)]
for name, stream, step in sources:
    shell = [r for r in map(json.loads, stream) if r['ok'] and r['fn'] in SHELL]
    for record in shell[::step]:
        text = FUNCTIONS[record['fn']](**record['kwargs'])
        with open(os.path.join(directory, f"{name}-{record['id']}.sh"), 'w', encoding='utf-8', newline='') as out:
            out.write(text)
