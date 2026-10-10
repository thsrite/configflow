"""Flask API 主应用 - 重构版本

这是重构后的精简版 backend.py，展示如何使用模块化的路由结构。
使用方法：
1. 确保所有路由模块都已创建并测试通过
2. 将此文件重命名为 backend.py（备份原文件）
3. 重启应用
"""
from flask import Flask
from flask_cors import CORS
import os
import logging
from logging.handlers import RotatingFileHandler
from pathlib import Path

# 导入公共模块
from backend.common.auth import validate_required_env_vars
from backend.common.config import load_config, DATA_DIR
from backend.common.agent_manager import init_agent_manager

# 导入版本信息
from backend.version import get_version_info

# 导入路由注册函数
from backend.routes import register_blueprints
from backend.routes.auth import setup_before_request

# 导入 MCP 服务端
from backend.mcp_server import mcp_bp

app = Flask(__name__)

CORS(app)

# 配置日志级别（从环境变量读取，默认为 INFO）
LOG_LEVEL = os.environ.get('LOG_LEVEL', 'INFO').upper()
log_level_map = {
    'DEBUG': logging.DEBUG,
    'INFO': logging.INFO,
    'WARNING': logging.WARNING,
    'ERROR': logging.ERROR,
    'CRITICAL': logging.CRITICAL
}
log_level = log_level_map.get(LOG_LEVEL, logging.INFO)

# 确保日志目录存在
log_dir = Path(DATA_DIR)
log_dir.mkdir(exist_ok=True, parents=True)

# 日志文件路径
log_file = os.environ.get('LOG_FILE', str(log_dir / 'app.log'))

# 配置日志格式
log_formatter = logging.Formatter('%(asctime)s - %(name)s - %(levelname)s - %(message)s')

# 配置文件处理器（带日志轮转，最大10MB，保留5个备份）
file_handler = RotatingFileHandler(
    log_file,
    maxBytes=10 * 1024 * 1024,  # 10MB
    backupCount=5,
    encoding='utf-8'
)
file_handler.setLevel(log_level)
file_handler.setFormatter(log_formatter)

# 配置控制台处理器
console_handler = logging.StreamHandler()
console_handler.setLevel(log_level)
console_handler.setFormatter(log_formatter)

# 配置根日志记录器
logging.basicConfig(
    level=log_level,
    handlers=[file_handler, console_handler]
)

app.logger.setLevel(log_level)

# 配置 werkzeug 日志级别（用于 Flask 开发服务器的 HTTP 请求日志）
# 只在 DEBUG 模式下显示 HTTP 请求日志，其他情况下完全隐藏
werkzeug_logger = logging.getLogger('werkzeug')
if LOG_LEVEL == 'DEBUG':
    werkzeug_logger.setLevel(logging.DEBUG)
else:
    # 设置为 ERROR 级别以完全隐藏 INFO 级别的 HTTP 请求日志
    werkzeug_logger.setLevel(logging.ERROR)
    # 禁用传播到父 logger，防止 basicConfig 的处理器输出日志
    werkzeug_logger.propagate = False

# 验证环境变量
validate_required_env_vars()

# 加载配置
load_config()

# 注册所有蓝图
register_blueprints(app)

# 注册 MCP 端点（供外部 MCP 客户端调用）
app.register_blueprint(mcp_bp)

# 设置全局认证中间件
setup_before_request(app)

# 应用初始化标志（确保只初始化一次）
_app_initialized = False


def initialize_app():
    """初始化应用"""
    global _app_initialized

    if _app_initialized:
        return

    _app_initialized = True

    try:
        # 初始化 Agent 管理器
        init_agent_manager()
        app.logger.info("✅ 应用初始化完成")
    except Exception as e:
        app.logger.error(f"❌ 应用初始化失败: {str(e)}")


# 在模块加载时自动初始化（适用于被导入的情况，如 Docker/Gunicorn）
initialize_app()


if __name__ == '__main__':
    # 启动信息（仅在直接运行时显示）
    print('\n' + '=' * 60)
    print('🚀 Config Flow API Server')
    print('=' * 60)
    version_info = get_version_info()
    print(f"Version: {version_info.get('version', 'unknown')}")
    print(f"Build: {version_info.get('build_time', 'unknown')}")
    print(f"Log Level: {LOG_LEVEL}")
    print('=' * 60)
    print('Server starting on http://0.0.0.0:5001')
    print('Press Ctrl+C to stop')
    print('=' * 60 + '\n')

    # 域名发现的自动探测（各配置空间单独开关，默认关闭）。只在作为服务进程运行时启动，
    # 调试模式的 reloader 父进程不启动；多进程时由文件锁保证只有一个在跑
    if LOG_LEVEL != 'DEBUG' or os.environ.get('WERKZEUG_RUN_MAIN') == 'true':
        from backend.utils.domain_discovery_service import start_auto_loop
        start_auto_loop(os.path.join(str(DATA_DIR), 'probes'))

    app.run(host='0.0.0.0', port=5001, debug=(LOG_LEVEL == 'DEBUG'))
