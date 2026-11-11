DELETE FROM iam.workload_credentials WHERE organization_id IS NULL;
DELETE FROM iam.workloads WHERE organization_id IS NULL;
DELETE FROM iam.principals WHERE organization_id IS NULL;

DROP INDEX IF EXISTS iam.idx_principals_platform_name;
ALTER TABLE iam.workload_credentials DROP COLUMN IF EXISTS public_key;
ALTER TABLE iam.workload_credentials DROP CONSTRAINT IF EXISTS fk_workload_credentials_workload;
ALTER TABLE iam.workloads DROP CONSTRAINT IF EXISTS fk_workloads_principal;
ALTER TABLE iam.workload_credentials ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE iam.workloads ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE iam.principals DROP CONSTRAINT IF EXISTS chk_principals_platform_workload;
ALTER TABLE iam.principals ALTER COLUMN organization_id SET NOT NULL;
