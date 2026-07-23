package store

const assetColumns = `
	id, created_by_user_id, subject_id, kind, status, storage_backend,
	staging_key, published_key, expected_size, actual_size, content_type,
	expires_at, width, height, error_message, created_at, updated_at,
	deleted_at
`

const createAssetStatement = `
	INSERT INTO assets (
		id, created_by_user_id, subject_id, kind, status, storage_backend,
		staging_key, published_key, expected_size, content_type, expires_at,
		created_at, updated_at
	) VALUES (
		:id, :created_by_user_id, :subject_id, :kind, :status, :storage_backend,
		:staging_key, :published_key, :expected_size, :content_type, :expires_at,
		:created_at, :updated_at
	)
`

const lockUploadQuotaScopeStatement = `
	SELECT pg_advisory_xact_lock(hashtextextended($1, 0))
`

const countActiveUploadsQuery = `
	SELECT COUNT(*)
	FROM assets
	WHERE created_by_user_id = $1
	  AND status = 'CREATED'
	  AND deleted_at = 0
`

const getAssetQuery = `
	SELECT ` + assetColumns + `
	FROM assets
	WHERE id = $1
	  AND deleted_at = 0
	LIMIT 1
`

const updateAssetStatement = `
	UPDATE assets
	SET status = :status,
		storage_backend = :storage_backend,
		published_key = :published_key,
		actual_size = :actual_size,
		content_type = :content_type,
		width = :width,
		height = :height,
		error_message = :error_message,
		updated_at = :updated_at
	WHERE id = :id
	  AND deleted_at = 0
`

const listExpiredUploadsQuery = `
	SELECT ` + assetColumns + `
	FROM assets
	WHERE status = 'CREATED'
	  AND expires_at > 0
	  AND expires_at < $1
	  AND deleted_at = 0
	LIMIT 100
`

const lockAssetStatement = `
	SELECT pg_advisory_lock(hashtextextended($1, 0))
`

const unlockAssetStatement = `
	SELECT pg_advisory_unlock(hashtextextended($1, 0))
`
