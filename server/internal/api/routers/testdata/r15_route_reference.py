"""Werkzeug resolves the actual overlapping Flask rules, without calling handlers."""
import json
import sys
from flask import Flask
from backend.routes import agents_bp
from backend.routes import agents, client_tools, gateway_installations, upstream_router_pending
app = Flask(__name__)
app.register_blueprint(agents_bp)
matcher = app.url_map.bind('localhost')
print(json.dumps([matcher.match(row['path'], method=row['method'])[0] for row in json.load(sys.stdin)]))
