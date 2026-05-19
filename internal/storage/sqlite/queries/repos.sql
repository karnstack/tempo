-- name: CreateRepo :one
INSERT INTO repos (tenant_id, connection_id, gh_id, owner, name, default_branch, archived)
VALUES (@tenant_id, @connection_id, @gh_id, @owner, @name, @default_branch, @archived)
RETURNING *;

-- name: GetRepo :one
SELECT * FROM repos WHERE id = @id;

-- name: GetRepoByGhID :one
SELECT * FROM repos WHERE tenant_id = @tenant_id AND gh_id = @gh_id;

-- name: GetRepoByTenantOwnerName :one
SELECT * FROM repos
WHERE tenant_id = @tenant_id AND owner = @owner AND name = @name
LIMIT 1;

-- name: ListReposByConnection :many
SELECT * FROM repos WHERE connection_id = @connection_id ORDER BY owner, name;

-- name: ListReposByTenant :many
SELECT * FROM repos WHERE tenant_id = @tenant_id ORDER BY owner, name;

-- name: ListAllRepos :many
SELECT * FROM repos ORDER BY id;

-- name: UpdateRepoArchived :exec
UPDATE repos SET archived = @archived WHERE id = @id;

-- name: DeleteRepo :exec
DELETE FROM repos WHERE id = @id;
