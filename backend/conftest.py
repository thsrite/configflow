"""Keep backend tests independent of local data and other pytest workers."""
from pathlib import Path
from tempfile import TemporaryDirectory

import pytest


@pytest.hookimpl(tryfirst=True)
def pytest_configure(config):
    # config.py resolves these paths during import, before fixtures can run.
    # A fresh directory also separates concurrent serial pytest invocations.
    worker = getattr(config, 'workerinput', {}).get('workerid', 'main')
    directory = TemporaryDirectory(prefix=f'configflow-pytest-{worker}-')
    environment = pytest.MonkeyPatch()
    environment.setenv('DATA_DIR', directory.name)
    environment.setenv('LOG_FILE', str(Path(directory.name) / 'app.log'))
    config.add_cleanup(directory.cleanup)
    config.add_cleanup(environment.undo)


@pytest.fixture(autouse=True)
def isolated_agent_metrics(tmp_path, monkeypatch):
    # MetricsHistory has its own default outside DATA_DIR. Keep its real I/O,
    # but direct AgentManager's default history into this test's directory.
    from backend.agents import manager
    from backend.agents.metrics_history import MetricsHistory

    def metrics_history(data_dir=None):
        return MetricsHistory(data_dir if data_dir is not None else str(tmp_path / 'metrics'))

    monkeypatch.setattr(manager, 'MetricsHistory', metrics_history)


@pytest.fixture(autouse=True)
def fresh_agent_runtime_state():
    # Heartbeat state lives in process memory; start each test as a fresh process would.
    from backend.agents import runtime_state
    runtime_state.reset()
    yield
    runtime_state.reset()
