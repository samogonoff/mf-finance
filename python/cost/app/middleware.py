from __future__ import annotations

from fastapi import HTTPException, Request

from app.roles import get_user_permissions


def require_perm(permission: str):
    """FastAPI dependency: checks that the authenticated user has the given permission."""
    async def _check(request: Request) -> str:
        email = request.headers.get("X-Cost-User", "")
        if not email:
            raise HTTPException(401, "Не передан заголовок X-Cost-User")
        perms = await get_user_permissions(email)
        if permission not in perms:
            raise HTTPException(403, f"Недостаточно прав: требуется «{permission}»")
        return email
    return _check
