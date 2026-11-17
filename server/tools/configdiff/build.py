"""Build the public Go command with its existing test-route manifest hook.

Business implementations and lifecycle sources are neither opened nor patched.
Both production servers retain their real startup/background behavior (X08).
"""
import pathlib
import subprocess
import sys


def build(root, output):
    root, output = pathlib.Path(root).resolve(), pathlib.Path(output).resolve()
    output.mkdir(parents=True, exist_ok=True)
    subprocess.run(['go', 'build', '-tags=configflow_proddiff',
                    '-o', str(output / 'configflow'), './cmd/configflow'], cwd=root / 'server', check=True)
    return output / 'configflow'


if __name__ == '__main__':
    print(build(*sys.argv[1:]))
