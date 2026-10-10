"""路由模块"""
from flask import Blueprint

# 创建各个功能模块的蓝图
auth_bp = Blueprint('auth', __name__, url_prefix='/api/auth')
subscriptions_bp = Blueprint('subscriptions', __name__, url_prefix='/api/subscriptions')
subscription_aggregations_bp = Blueprint('subscription_aggregations', __name__, url_prefix='/api/aggregations')
nodes_bp = Blueprint('nodes', __name__, url_prefix='/api/nodes')
rules_bp = Blueprint('rules', __name__, url_prefix='/api/rules')
rule_sets_bp = Blueprint('rule_sets', __name__, url_prefix='/api/rule-sets')  # 规则集（旧接口，向后兼容）
proxy_groups_bp = Blueprint('proxy_groups', __name__, url_prefix='/api/proxy-groups')
generate_bp = Blueprint('generate', __name__, url_prefix='/api/generate')
config_bp = Blueprint('config', __name__, url_prefix='/api/config')
custom_config_bp = Blueprint('custom_config', __name__, url_prefix='/api/custom-config')
agents_bp = Blueprint('agents', __name__, url_prefix='/api/agents')
mosdns_bp = Blueprint('mosdns', __name__, url_prefix='/api/mosdns')
settings_bp = Blueprint('settings', __name__, url_prefix='/api')
logs_bp = Blueprint('logs', __name__, url_prefix='/api/logs')
stats_bp = Blueprint('stats', __name__, url_prefix='/api/stats')
profiles_bp = Blueprint('profiles', __name__, url_prefix='/api/profiles')


def register_blueprints(app):
    """注册所有蓝图到 Flask 应用

    注意：必须在导入蓝图之后再导入路由模块，以确保路由函数被注册到蓝图上
    """
    # 导入各个路由模块（这会将路由函数注册到对应的蓝图）
    from backend.routes import auth
    from backend.routes import subscriptions
    from backend.routes import aggregations
    from backend.routes import nodes
    from backend.routes import rules
    from backend.routes.rule_library import rule_library_bp  # 独立蓝图
    from backend.routes import proxy_groups
    from backend.routes import generate
    from backend.routes import config  # 包含 custom_config 路由
    from backend.routes import agents
    from backend.routes import mosdns
    from backend.routes import settings
    from backend.routes.logs import logs_bp  # 日志路由
    from backend.routes.stats import stats_bp  # 统计路由
    from backend.routes import profiles
    from backend.routes.domain_discovery import domain_discovery_bp

    # 注册所有蓝图
    app.register_blueprint(auth_bp)
    app.register_blueprint(subscriptions_bp)
    app.register_blueprint(subscription_aggregations_bp)
    app.register_blueprint(nodes_bp)
    app.register_blueprint(rules_bp)
    app.register_blueprint(rule_sets_bp)  # 规则集（向后兼容）
    app.register_blueprint(rule_library_bp)
    app.register_blueprint(proxy_groups_bp)
    app.register_blueprint(generate_bp)
    app.register_blueprint(config_bp)
    app.register_blueprint(custom_config_bp)
    app.register_blueprint(agents_bp)
    app.register_blueprint(mosdns_bp)
    app.register_blueprint(settings_bp)
    app.register_blueprint(logs_bp)  # 注册日志路由
    app.register_blueprint(stats_bp)  # 注册统计路由
    app.register_blueprint(profiles_bp)
    app.register_blueprint(domain_discovery_bp)

    from backend.common.config_repository import (
        ProfileRepositoryError, ProfileNotFound, ProfileValidationError,
        ProfileExists, ProfileInUse,
    )
    from backend.common.profile_context import install_profile_context

    @app.errorhandler(ProfileValidationError)
    def _profile_validation_error(error):
        return {'success': False, 'message': str(error)}, 400

    @app.errorhandler(ProfileNotFound)
    def _profile_not_found(error):
        return {'success': False, 'message': f'Profile not found: {error.args[0]}'}, 404

    @app.errorhandler(ProfileExists)
    @app.errorhandler(ProfileInUse)
    def _profile_conflict(error):
        payload = {'success': False, 'message': str(error)}
        if isinstance(error, ProfileInUse):
            payload['usages'] = error.usages
        return payload, 409

    @app.errorhandler(ProfileRepositoryError)
    def _profile_repository_error(error):
        return {'success': False, 'message': str(error)}, 400

    install_profile_context(app)
