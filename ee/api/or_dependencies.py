import json
from typing import Callable

from fastapi import HTTPException, Depends
from fastapi import Security
from fastapi.routing import APIRoute
from fastapi.security import SecurityScopes
from starlette import status
from starlette.exceptions import HTTPException
from starlette.requests import Request
from starlette.responses import Response, JSONResponse

import schemas_ee
from chalicelib.core import traces


async def OR_context(request: Request) -> schemas_ee.CurrentContext:
    if hasattr(request.state, "currentContext"):
        return request.state.currentContext
    else:
        raise Exception("currentContext not found")


class ORRoute(APIRoute):
    def get_route_handler(self) -> Callable:
        original_route_handler = super().get_route_handler()

        async def custom_route_handler(request: Request) -> Response:
            try:
                response: Response = await original_route_handler(request)
            except HTTPException as e:
                if e.status_code // 100 == 4:
                    response = JSONResponse(content={"errors": [e.detail]}, status_code=e.status_code)
                else:
                    raise e

            if isinstance(response, JSONResponse):
                response: JSONResponse = response
                body = json.loads(response.body.decode('utf8'))
                if response.status_code == 200 and body is not None and body.get("errors") is not None:
                    if "not found" in body["errors"][0]:
                        response.status_code = status.HTTP_404_NOT_FOUND
                    else:
                        response.status_code = status.HTTP_400_BAD_REQUEST
            traces.trace(action=self.name, path_format=self.path_format, request=request, response=response)
            return response

        return custom_route_handler


def __check(security_scopes: SecurityScopes, context: schemas_ee.CurrentContext = Depends(OR_context)):
    for scope in security_scopes.scopes:
        if scope not in context.permissions:
            raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED,
                                detail="Not enough permissions")


def OR_scope(*scopes):
    return Security(__check, scopes=list(scopes))
