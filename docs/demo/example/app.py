"""A small service used by the llm-data demo."""

import os

DEFAULT_PORT = 8080


def start_server(host, port):
    return {"host": host, "port": port, "debug": os.environ.get("DEBUG") == "1"}
