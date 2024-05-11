import json
from main import app
import codefly_sdk.codefly as codefly
from fastapi.openapi.utils import get_openapi

if __name__ == "__main__":
    openapi_schema = get_openapi(
        title=codefly.get_service(),
        version=codefly.get_version(),
        routes=app.routes,
    )
    app.openapi_schema = openapi_schema
    openapi = app.openapi()
    with open("../openapi/api.json", "w") as f:
        f.write(json.dumps(openapi))
