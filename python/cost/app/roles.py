from __future__ import annotations

import json

from app.db import pool


def _parse_role(row: dict) -> dict:
    row = dict(row)
    perms = row.get("permissions")
    if isinstance(perms, str):
        row["permissions"] = json.loads(perms)
    return row


async def get_roles() -> list[dict]:
    """SELECT * FROM cost_roles ORDER BY id."""
    async with pool().acquire() as conn:
        rows = await conn.fetch("SELECT * FROM cost_roles ORDER BY id")
        return [_parse_role(r) for r in rows]


async def create_role(name: str, permissions: list[str]) -> dict:
    """INSERT INTO cost_roles, RETURNING *."""
    async with pool().acquire() as conn:
        row = await conn.fetchrow(
            "INSERT INTO cost_roles (name, permissions) VALUES ($1, $2::jsonb) RETURNING *",
            name,
            json.dumps(permissions),
        )
        return _parse_role(row)


async def update_role(role_id: int, name: str | None, permissions: list[str] | None) -> dict | None:
    """UPDATE cost_roles SET ... RETURNING *."""
    async with pool().acquire() as conn:
        if name is not None and permissions is not None:
            row = await conn.fetchrow(
                "UPDATE cost_roles SET name = $1, permissions = $2::jsonb WHERE id = $3 RETURNING *",
                name,
                json.dumps(permissions),
                role_id,
            )
        elif name is not None:
            row = await conn.fetchrow(
                "UPDATE cost_roles SET name = $1 WHERE id = $2 RETURNING *",
                name,
                role_id,
            )
        elif permissions is not None:
            row = await conn.fetchrow(
                "UPDATE cost_roles SET permissions = $1::jsonb WHERE id = $2 RETURNING *",
                json.dumps(permissions),
                role_id,
            )
        else:
            row = await conn.fetchrow(
                "SELECT * FROM cost_roles WHERE id = $1",
                role_id,
            )
        return _parse_role(row) if row else None


async def delete_role(role_id: int) -> bool:
    """DELETE FROM cost_roles WHERE id=$1 AND is_system=FALSE. Return whether deleted."""
    async with pool().acquire() as conn:
        result = await conn.execute(
            "DELETE FROM cost_roles WHERE id = $1 AND is_system = FALSE",
            role_id,
        )
        return result == "DELETE 1"


async def get_user_roles(email: str | None = None) -> list[dict]:
    """SELECT cur.*, cr.name, cr.permissions FROM cost_user_roles cur JOIN cost_roles cr ON cur.role_id = cr.id."""
    async with pool().acquire() as conn:
        if email:
            rows = await conn.fetch(
                "SELECT cur.*, cr.name AS role_name, cr.permissions "
                "FROM cost_user_roles cur "
                "JOIN cost_roles cr ON cur.role_id = cr.id "
                "WHERE cur.email = $1",
                email,
            )
        else:
            rows = await conn.fetch(
                "SELECT cur.*, cr.name AS role_name, cr.permissions "
                "FROM cost_user_roles cur "
                "JOIN cost_roles cr ON cur.role_id = cr.id"
            )
        return [_parse_role(r) for r in rows]


async def assign_role(email: str, role_id: int, granted_by: str = "") -> dict:
    """INSERT INTO cost_user_roles ... RETURNING *."""
    async with pool().acquire() as conn:
        row = await conn.fetchrow(
            "INSERT INTO cost_user_roles (email, role_id, granted_by) VALUES ($1, $2, $3) ON CONFLICT (email, role_id) DO NOTHING RETURNING *",
            email,
            role_id,
            granted_by,
        )
        if not row:
            raise ValueError("Пользователь уже имеет эту роль")
        return dict(row)


async def remove_user_role(assignment_id: int) -> bool:
    """DELETE FROM cost_user_roles WHERE id=$1. Return whether deleted."""
    async with pool().acquire() as conn:
        result = await conn.execute(
            "DELETE FROM cost_user_roles WHERE id = $1",
            assignment_id,
        )
        return result == "DELETE 1"


async def get_user_permissions(email: str) -> list[str]:
    """SELECT DISTINCT jsonb_array_elements_text(cr.permissions) ... Returns flat list of permission strings."""
    async with pool().acquire() as conn:
        rows = await conn.fetch(
            "SELECT DISTINCT jsonb_array_elements_text(cr.permissions) AS perm "
            "FROM cost_user_roles cur "
            "JOIN cost_roles cr ON cur.role_id = cr.id "
            "WHERE cur.email = $1",
            email,
        )
        return [r["perm"] for r in rows]
