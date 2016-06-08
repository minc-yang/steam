package data

import (
	"database/sql"
	"fmt"
	"github.com/h2oai/steamY/master/az"
	"github.com/lib/pq"
	"strings"
)

var Permissions []Permission
var EntityTypes []EntityType

func init() {
	Permissions = []Permission{
		{0, "role.manage", "Manage roles"},
		{0, "role.view", "View roles"},
		{0, "workgroup.manage", "Manage workgroups"},
		{0, "workgroup.view", "View workgroups"},
		{0, "identity.manage", "Manage identities"},
		{0, "identity.view", "View identities"},
		{0, "engine.manage", "Manage engines"},
		{0, "engine.view", "View engines"},
		{0, "cluster.manage", "Manage clusters"},
		{0, "cluster.view", "View clusters"},
		{0, "model.manage", "Manage models"},
		{0, "model.view", "View models"},
	}
	EntityTypes = []EntityType{
		{0, "role"},
		{0, "workgroup"},
		{0, "identity"},
		{0, "engine"},
		{0, "cluster"},
		{0, "model"},
	}
}

type Datastore struct {
	db              *sql.DB // Singleton; doesn't actually connect until used, and is pooled internally.
	metadata        map[string]string
	permissions     []Permission
	permissionMap   map[int64]Permission
	entityTypeMap   map[int64]EntityType
	entityTypeIdMap map[string]int64
}

const (
	Version = "1"
)

// NewDB creates a new instance of a data access object.
//
// Valid values for sslmode are:
//   disable - No SSL
//   require - Always SSL (skip verification)
//   verify-ca - Always SSL (verify that the certificate presented by the server was signed by a trusted CA)
//   verify-full - Always SSL (verify that the certification presented by the server was signed by a
//     trusted CA and the server host name matches the one in the certificate)
func NewDatastore(username, dbname, sslmode string) (*Datastore, error) {

	// Open connection

	db, err := sql.Open("postgres", fmt.Sprintf("user=%s dbname=%s sslmode=%s", username, dbname, sslmode))
	if err != nil {
		return nil, fmt.Errorf("Database connection failed: %s", err)
	}

	// TODO can use db.SetMaxOpenConns() and db.SetMaxIdleConns() to configure further.

	// Verify connection

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("Database ping failed: %s", err)
	}

	// Read meta information

	metadata, err := readMetadata(db)
	if err != nil {
		return nil, err
	}

	// Get version; prime if pristine

	version, ok := metadata["version"]
	if !ok {
		prime(db)
	}

	upgrade(db, version)

	permissions, err := readPermissions(db)
	if err != nil {
		return nil, err
	}

	permissionMap := make(map[int64]Permission)
	for _, permission := range permissions {
		permissionMap[permission.Id] = permission
	}

	entityTypeMap, err := readEntityTypes(db)
	if err != nil {
		return nil, err
	}

	entityTypeIdMap := make(map[string]int64)
	for id, entityType := range entityTypeMap {
		entityTypeIdMap[entityType.Name] = id
	}

	return &Datastore{
		db,
		metadata,
		permissions,
		permissionMap,
		entityTypeMap,
		entityTypeIdMap,
	}, nil
}

func prime(db *sql.DB) error {
	if err := createMetadata(db, "version", "1"); err != nil {
		return err
	}
	if err := primePermissions(db, Permissions); err != nil {
		return err
	}
	if err := primeEntityTypes(db, EntityTypes); err != nil {
		return err
	}

	return nil
}

func bulkInsert(db *sql.DB, table string, columns []string, f func(*sql.Stmt) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(pq.CopyIn(table, columns...))
	if err != nil {
		return err
	}

	if err := f(stmt); err != nil { // buffer
		return err
	}

	if _, err := stmt.Exec(); err != nil { // flush
		return err
	}

	if err := stmt.Close(); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func createMetadata(db *sql.DB, key, value string) error {
	_, err := db.Exec(`
		INSERT INTO
			meta
			(key, value)
		VALUES
			($1,  $2)
		`, key, value)
	return err
}

func primeEntityTypes(db *sql.DB, entityTypes []EntityType) error {
	return bulkInsert(db, "entity_type", []string{"name"}, func(stmt *sql.Stmt) error {
		for _, entityType := range entityTypes {
			_, err := stmt.Exec(entityType.Name)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func primePermissions(db *sql.DB, permissions []Permission) error {
	return bulkInsert(db, "permission", []string{"name", "description"}, func(stmt *sql.Stmt) error {
		for _, permission := range permissions {
			_, err := stmt.Exec(permission.Name, permission.Description)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func upgrade(db *sql.DB, currentVersion string) error {
	if currentVersion == Version {
		return nil
	}

	// TODO add stepwise upgrades

	return nil
}

func truncate(db *sql.DB) error {
	return executeTransaction(db, func(tx *sql.Tx) error {
		tables := []string{
			"history",
			"privilege",
			"role_permission",
			"identity_role",
			"identity_workgroup",
			"workgroup",
			"role",
			"identity",
			"permission",
			"entity_type",
			"meta",
		}
		for _, table := range tables {
			if _, err := tx.Exec("DELETE FROM " + table); err != nil {
				return err
			}
		}
		return nil
	})
}

func (ds *Datastore) Prime() error {
	return prime(ds.db)
}

func (ds *Datastore) Truncate() error {
	return truncate(ds.db)
}

// --- Superuser ---

func (ds *Datastore) SetupSuperuser(name, password string) (int64, error) {
	roleId, err := ds.CreateRole(nil, "Superuser", "Superuser")
	if err != nil {
		return 0, nil
	}

	allPerms := make([]int64, len(ds.permissions))
	for i, permission := range ds.permissions {
		allPerms[i] = permission.Id
	}

	if err := ds.SetRolePermissions(nil, roleId, allPerms); err != nil {
		return 0, err
	}

	return ds.CreateIdentity(nil, name, password)
}

// --- Lookup tables (static) ---

func readMetadata(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`
		SELECT
			id, key, value
		FROM
			meta
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries, err := ScanMetas(rows)
	if err != nil {
		return nil, err
	}

	lookup := make(map[string]string)
	for _, entry := range entries {
		lookup[entry.Key] = entry.Value
	}

	return lookup, nil
}

func readEntityTypes(db *sql.DB) (map[int64]EntityType, error) {
	rows, err := db.Query(`
		SELECT 
			id, name 
		FROM 
			entity_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entityTypes, err := ScanEntityTypes(rows)
	if err != nil {
		return nil, err
	}

	lookup := make(map[int64]EntityType)
	for _, et := range entityTypes {
		lookup[et.Id] = et
	}

	return lookup, nil
}

func executeTransaction(db *sql.DB, f func(*sql.Tx) error) (err error) {
	var (
		tx     *sql.Tx
		commit bool
	)

	tx, err = db.Begin()
	if err != nil {
		return err
	}

	defer func() {
		if commit {
			err = tx.Commit()
		} else {
			if rberr := tx.Rollback(); rberr != nil {
				err = fmt.Errorf("Rollback failure: %s (after %s)", rberr, err)
			}
		}
	}()
	err = f(tx)
	if err == nil {
		commit = true
	}
	return err

}

func (ds *Datastore) exec(f func(*sql.Tx) error) (err error) {
	return executeTransaction(ds.db, f)
}

func (ds *Datastore) toEntityTypeId(name string) (int64, error) {
	if id, ok := ds.entityTypeIdMap[name]; ok {
		return id, nil
	}
	return 0, fmt.Errorf("Invalid entity type: %s", name)
}

// --- History ---

func (ds *Datastore) audit(principal *az.Principal, tx *sql.Tx, action, entityType string, entityId int64, description string) error {
	if principal == nil {
		return nil
	}
	entityTypeId, err := ds.toEntityTypeId(entityType)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO 
			history 
			(identity_id, action, entity_type_id, entity_id, description, created) 
		VALUES  
			($1,          $2,     $3,             $4,        $5,          now())
		`, principal.Id, action, entityTypeId, entityId, description); err != nil {
		return err
	}
	return nil
}

// --- Permissions ---

func readPermissions(db *sql.DB) ([]Permission, error) {
	rows, err := db.Query(`
		SELECT 
			id, name, description 
		FROM 
			permission
		ORDER BY
			name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return ScanPermissions(rows)
}

func (ds *Datastore) ReadPermissions(principal *az.Principal) ([]Permission, error) {
	return ds.permissions, nil
}

// --- Role ---

func (ds *Datastore) CreateRole(principal *az.Principal, name, description string) (int64, error) {
	var id int64
	err := ds.exec(func(tx *sql.Tx) error {
		row := tx.QueryRow(`
			INSERT INTO 
				role   
				(name, description, created) 
			VALUES 
				($1,   $2,          now()) 
			RETURNING id
			`, name, description)
		if err := row.Scan(&id); err != nil {
			return err
		}

		return ds.audit(principal, tx, "create", "role", id, "")
	})
	return id, err
}

func (ds *Datastore) ReadRoles(principal *az.Principal, offset, limit int64) ([]Role, error) {
	rows, err := ds.db.Query(`
		SELECT 
			id, name, description, created 
		FROM 
			role 
		ORDER BY name 
		OFFSET $1 
		LIMIT $2
		`, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return ScanRoles(rows)
}

func (ds *Datastore) ReadRole(principal *az.Principal, roleId int64) (Role, error) {
	row := ds.db.QueryRow(`
		SELECT 
			id, name, description, created 
		FROM 
			role 
		WHERE 
			id = $1
		`, roleId)
	return ScanRole(row)
}

func (ds *Datastore) ReadPermissionsForRole(principal *az.Principal, roleId int64) ([]Permission, error) {
	rows, err := ds.db.Query(`
		SELECT
			p.id, p.name, p.description
		FROM
			role_permission rp,
			permission p
		WHERE
			rp.role_id = $1 AND
			rp.permission_id = p.id
		ORDER BY
			p.name
		`, roleId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return ScanPermissions(rows)
}

func (ds *Datastore) UpdateRole(principal *az.Principal, roleId int64, name string) error {
	return ds.exec(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			UPDATE 
				role 
			SET 
				name = $1 
			WHERE 
				id = $2
			`, name, roleId); err != nil {
			return err
		}
		return ds.audit(principal, tx, "update", "role", roleId, fmt.Sprintf("Set name to %s", name))
	})
}

func (ds *Datastore) toPermissionNames(ids []int64) ([]string, error) {
	names := make([]string, len(ids))
	for i, id := range ids {
		if p, ok := ds.permissionMap[id]; ok {
			names[i] = p.Name
		} else {
			return names, fmt.Errorf("Invalid permission id: %d", id)
		}
	}
	return names, nil
}

func (ds *Datastore) SetRolePermissions(principal *az.Principal, roleId int64, permissionIds []int64) error {
	return ds.exec(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			DELETE FROM 
				role_permission 
			WHERE 
				role_id = $1
			`, roleId); err != nil {
			return err
		}

		for _, permissionId := range permissionIds {
			if _, err := tx.Exec(`
				INSERT INTO 
					role_permission 
				VALUES 
					($1, $2)
				`, roleId, permissionId); err != nil {
				return err
			}
		}

		permissionNames, err := ds.toPermissionNames(permissionIds)
		if err != nil {
			return err
		}
		return ds.audit(principal, tx, "update", "role", roleId, fmt.Sprintf("Set permissions to: %s", strings.Join(permissionNames, ", ")))
	})
}

func (ds *Datastore) DeleteRole(principal *az.Principal, roleId int64) error {
	return ds.exec(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			DELETE FROM 
				role 
			WHERE 
				id = $1
			`, roleId); err != nil {
			return err
		}
		return ds.audit(principal, tx, "delete", "role", roleId, "")
	})
}

// --- Workgroup ---

func (ds *Datastore) CreateWorkgroup(principal *az.Principal, name, description string) (int64, error) {
	var id int64
	err := ds.exec(func(tx *sql.Tx) error {
		row := tx.QueryRow(`
			INSERT INTO 
				workgroup 
				(name, description, created) 
			VALUES 
				($1,   $2,          now()) 
			RETURNING id
			`, name, description)
		if err := row.Scan(&id); err != nil {
			return err
		}
		return ds.audit(principal, tx, "create", "workgroup", id, "")
	})
	return id, err
}

func (ds *Datastore) ReadWorkgroups(principal *az.Principal, offset, limit int64) ([]Workgroup, error) {
	rows, err := ds.db.Query(`
		SELECT 
			id, name, description, created 
		FROM 
			workgroup 
		ORDER BY name 
		OFFSET $1 
		LIMIT $2
		`, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return ScanWorkgroups(rows)
}

func (ds *Datastore) ReadWorkgroup(principal *az.Principal, workgroupId int64) (Workgroup, error) {
	row := ds.db.QueryRow(`
		SELECT 
			id, name, description, created 
		FROM 
			workgroup 
		WHERE 
			id = $1
		ORDER BY
			name
		`, workgroupId)

	return ScanWorkgroup(row)
}

func (ds *Datastore) ReadIdentitesForWorkgroup(principal *az.Principal, workgroupId int64) ([]Identity, error) {
	rows, err := ds.db.Query(`
		SELECT 
			i.id, i.name, i.is_active, i.last_login, i.created 
		FROM 
			identity i, 
			identity_workgroup iw 
		WHERE 
			iw.workgroup_id = $1 AND 
		  iw.identity_id = i.id
		ORDER BY
			i.name
		`, workgroupId)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return ScanIdentitys(rows)
}

func (ds *Datastore) ReadIdentitesForRole(principal *az.Principal, roleId int64) ([]Identity, error) {
	rows, err := ds.db.Query(`
		SELECT
			i.id, i.name, i.is_active, i.last_login, i.created
		FROM
			identity i,
			identity_role ir
		WHERE
			ir.role_id = $1 AND
		  ir.identity_id = i.id
		ORDER BY
			i.name
		`, roleId)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return ScanIdentitys(rows)
}

func (ds *Datastore) UpdateWorkgroup(principal *az.Principal, workgroupId int64, name string) error {
	return ds.exec(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			UPDATE 
				workgroup
			SET 
				name = $1 
			WHERE 
				id = $2
			`, name, workgroupId); err != nil {
			return err
		}
		return ds.audit(principal, tx, "update", "workgroup", workgroupId, fmt.Sprintf("Set name to %s", name))
	})
}

func (ds *Datastore) DeleteWorkgroup(principal *az.Principal, workgroupId int64) error {
	return ds.exec(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			DELETE FROM 
				workgroup 
			WHERE 
				id = $1
			`, workgroupId); err != nil {
			return err
		}
		return ds.audit(principal, tx, "delete", "workgroup", workgroupId, "")
	})
}

// --- Identity ---

func (ds *Datastore) CreateIdentity(principal *az.Principal, name, password string) (int64, error) {
	var id int64
	err := ds.exec(func(tx *sql.Tx) error {
		row := tx.QueryRow(`
			INSERT INTO 
				identity 
				(name, password, is_active, created) 
			VALUES 
				($1,   $2,       $3,        now()) 
			RETURNING id
			`, name, password, true)
		if err := row.Scan(&id); err != nil {
			return err
		}
		return ds.audit(principal, tx, "create", "identity", id, "")
	})
	return id, err
}

func (ds *Datastore) ReadIdentities(principal *az.Principal, offset, limit int64) ([]Identity, error) {
	rows, err := ds.db.Query(`
		SELECT 
			id, name, is_active, last_login, created 
		FROM 
			identity
		ORDER BY name 
		OFFSET $1 
		LIMIT $2
		`, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return ScanIdentitys(rows)
}

func (ds *Datastore) ReadIdentityAndPassword(principal *az.Principal, identityId int64) (IdentityAndPassword, error) {
	row := ds.db.QueryRow(`
		SELECT 
			id, name, password, is_active, last_login, created 
		FROM 
			identity
		WHERE
			id = $1
		`, identityId)
	return ScanIdentityAndPassword(row)
}

func (ds *Datastore) AssocIdentityToWorkgroup(principal *az.Principal, identityId, workgroupId int64) error {
	workgroup, err := ds.ReadWorkgroup(principal, workgroupId)
	if err != nil {
		return err
	}
	return ds.exec(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			INSERT INTO 
				identity_workgroup
			VALUES 
				($1, $2)
			`, identityId, workgroupId); err != nil {
			return err
		}
		return ds.audit(principal, tx, "update", "identity", identityId, fmt.Sprintf("Associate group %s", workgroup.Name))
	})
}

func (ds *Datastore) DissocIdentityFromWorkgroup(principal *az.Principal, identityId, workgroupId int64) error {
	workgroup, err := ds.ReadWorkgroup(principal, workgroupId)
	if err != nil {
		return err
	}
	return ds.exec(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			DELETE FROM 
				identity_workgroup
			WHERE 
				identity_id = $1 AND
				workgroup_id = $2
			`, identityId, workgroupId); err != nil {
			return err
		}
		return ds.audit(principal, tx, "update", "identity", identityId, fmt.Sprintf("Dissociate group %s", workgroup.Name))
	})
}

func (ds *Datastore) AssocIdentityToRole(principal *az.Principal, identityId, roleId int64) error {
	role, err := ds.ReadRole(principal, roleId)
	if err != nil {
		return err
	}
	return ds.exec(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			INSERT INTO 
				identity_role
			VALUES 
				($1, $2)
			`, identityId, roleId); err != nil {
			return err
		}
		return ds.audit(principal, tx, "update", "identity", identityId, fmt.Sprintf("Associate role %s", role.Name))
	})
}

func (ds *Datastore) DissocIdentityFromRole(principal *az.Principal, identityId, roleId int64) error {
	role, err := ds.ReadRole(principal, roleId)
	if err != nil {
		return err
	}
	return ds.exec(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			DELETE FROM 
				identity_role
			WHERE 
				identity_id = $1 AND
				role_id = $2
			`, identityId, roleId); err != nil {
			return err
		}
		return ds.audit(principal, tx, "update", "identity", identityId, fmt.Sprintf("Dissociate role %s", role.Name))
	})
}

func (ds *Datastore) ReadIdentity(principal *az.Principal, identityId int64) (Identity, error) {
	row := ds.db.QueryRow(`
		SELECT 
			id, name, is_active, last_login, created 
		FROM 
			identity
		WHERE
			id = $1
		`, identityId)

	return ScanIdentity(row)
}

func (ds *Datastore) ReadRolesForIdentity(principal *az.Principal, identityId int64) ([]Role, error) {
	rows, err := ds.db.Query(`
		SELECT 
			r.id, r.name, r.description, r.created
		FROM 
			role r, 
			identity_role ir 
		WHERE 
		  ir.identity_id = $1 AND
			ir.role_id = r.id
		ORDER BY
			r.name
		`, identityId)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return ScanRoles(rows)
}

func (ds *Datastore) ReadWorkgroupsForIdentity(principal *az.Principal, identityId int64) ([]Workgroup, error) {
	rows, err := ds.db.Query(`
		SELECT 
			w.id, w.name, w.description, w.created
		FROM 
			workgroup w, 
			identity_workgroup iw
		WHERE 
		  iw.identity_id = $1 AND
			iw.workgroup_id = w.id
		ORDER BY
			w.name
		`, identityId)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return ScanWorkgroups(rows)
}

func (ds *Datastore) DeactivateIdentity(principal *az.Principal, identityId int64) error {
	return ds.exec(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			UPDATE 
				identity 
			SET
				is_active = false
			WHERE 
				id = $1
			`, identityId); err != nil {
			return err
		}
		return ds.audit(principal, tx, "deactivate", "identity", identityId, "")
	})
	return nil
}

// --- Privileges ---

// select privilege_type
// from privilege
// where
//		entity_id = 234 and
//		entity_type_id = 5 and
//		((identity_type is individual and identity_id = 2345) OR
//		(identity_type is workgroup and identity_id in
//		(select workgroup_id from identity_workgroup where identity_id = 2345)
//		))