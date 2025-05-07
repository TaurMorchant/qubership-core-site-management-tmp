package pg

import (
	"context"
	"database/sql"
	pgdbaas "github.com/netcracker/qubership-core-lib-go-dbaas-postgres-client/v4"
	"github.com/netcracker/qubership-core-lib-go/v3/logging"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain"
	wrappers "github.com/netcracker/qubership-core-site-management/site-management-service/v2/domain/wrappers"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/exceptions"
	"github.com/netcracker/qubership-core-site-management/site-management-service/v2/utils"
	"github.com/uptrace/bun"
	"net/http"
)

type RouteManagerDao struct {
	pgClient pgdbaas.PgClient
}

var (
	logger logging.Logger
)

func init() {
	logger = logging.GetLogger("dao")
}

func NewRouteManager(client pgdbaas.PgClient) *RouteManagerDao {
	logger.Debug("Create new RouteManager")
	rm := &RouteManagerDao{pgClient: client}
	return rm
}

func (v *RouteManagerDao) usingDB(ctx context.Context, handler func(db *bun.DB) (interface{}, error)) (interface{}, error) {
	logger.DebugC(ctx, "Running usingCollection wrapper")
	db, err := v.pgClient.GetBunDb(ctx)

	if err != nil {
		logger.ErrorC(ctx, "Got error during acquiring connection from pgClient")
		return nil, err
	}

	logger.DebugC(ctx, "Start search for data from RouteManager: conn=%s", db)

	return handler(db)
}

func (v *RouteManagerDao) FindInitInformation(ctx context.Context) (*domain.Init, error) {
	res, err := v.usingDB(ctx, func(db *bun.DB) (interface{}, error) {
		var first domain.Init
		if err := db.NewSelect().Model(&first).Limit(1).Scan(ctx); err != nil {
			logger.InfoC(ctx, "Empty result or search error: %s", err)
			return &domain.Init{Initialized: true}, err
		} else {
			logger.InfoC(ctx, "Found init information")
			return &first, nil
		}
	})
	if err == nil {
		return res.(*domain.Init), err
	} else {
		return nil, err
	}

}

func (v *RouteManagerDao) SetInitInformation(ctx context.Context, data domain.Init) error {
	logger.InfoC(ctx, "Upsert init information: %s", data)
	_, err := v.usingDB(ctx, func(db *bun.DB) (interface{}, error) {
		_, err := db.NewInsert().Model(&data).Exec(ctx)
		return nil, err
	})
	return err
}

func (v *RouteManagerDao) FindAll(ctx context.Context) (*[]domain.TenantDns, error) {
	res, err := v.usingDB(ctx, func(db *bun.DB) (interface{}, error) {
		var result []domain.TenantDns

		err := db.NewSelect().Model(&result).Scan(ctx)
		if err != nil {
			return nil, err
		}
		logger.InfoC(ctx, "Found %d route(s)", len(result))
		return &result, nil

	})
	if err != nil {
		return nil, err
	}
	return res.(*[]domain.TenantDns), nil
}

func (v *RouteManagerDao) FindByTenantId(ctx context.Context, tenantId string) (*domain.TenantDns, error) {
	logger.InfoC(ctx, "Find routes by tenantId: %s", tenantId)
	res, err := v.usingDB(ctx, func(db *bun.DB) (interface{}, error) {
		result := &domain.TenantDns{TenantId: tenantId}
		if err := db.NewSelect().Model(result).WherePK().Scan(ctx); err != nil {
			logger.InfoC(ctx, "Empty result or search error: %s", err)
			return result, exceptions.NewTenantNotFoundError(tenantId)
		} else {
			logger.InfoC(ctx, "Found routes by tenantId: %s, docs: %s", tenantId, result)
			return result, nil
		}
	})
	return res.(*domain.TenantDns), err
}

func (v *RouteManagerDao) SaveTenant(ctx context.Context, tenant *domain.TenantDns) error {
	logger.InfoC(ctx, "Save tenant %v", tenant)

	_, err := v.usingDB(ctx, func(db *bun.DB) (interface{}, error) {
		_, err := db.NewInsert().Model(tenant).Exec(ctx)

		logger.InfoC(ctx, "Tenant save err: %s", err)
		return nil, err
	})
	return err
}

func (v *RouteManagerDao) Upsert(ctx context.Context, data domain.TenantDns) error {
	logger.InfoC(ctx, "Upsert routes: %s", &data)
	_, err := v.usingDB(ctx, func(db *bun.DB) (interface{}, error) {
		tenantDns := &domain.TenantDns{TenantId: data.TenantId}
		err := db.NewSelect().Model(tenantDns).WherePK().Scan(ctx)
		if err == nil { // Update
			if _, err := db.NewUpdate().Model(&data).WherePK().Exec(ctx); err != nil {
				logger.ErrorC(ctx, "Error during Update %+v", err.Error())
				return nil, err
			}
		} else if err == sql.ErrNoRows { // Insert
			if _, err := db.NewInsert().Model(&data).Exec(ctx); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}

		logger.InfoC(ctx, "Routes save result: %s", &data)
		return nil, nil
	})
	return err
}

func (v *RouteManagerDao) Delete(ctx context.Context, tenantId string) error {
	logger.InfoC(ctx, "Delete routes by tenantId: %s", tenantId)
	_, err := v.usingDB(ctx, func(db *bun.DB) (interface{}, error) {
		tenantForDeletion := &domain.TenantDns{TenantId: tenantId}
		_, err := db.NewDelete().Model(tenantForDeletion).WherePK().Exec(ctx)
		logger.InfoC(ctx, "Routes deletion err: %s", err)
		return nil, err
	})
	return err
}

func (v *RouteManagerDao) AddRouteToTenants(ctx context.Context, host, serviceName string) error {
	logger.InfoC(ctx, "Update tenants with host %s and service %s", host, serviceName)

	_, err := v.usingDB(ctx, func(db *bun.DB) (interface{}, error) {
		err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			var tenants []domain.TenantDns
			err := tx.NewSelect().Model(&tenants).Where("active=?", true).Scan(ctx)
			if err != nil {
				return err
			}
			for _, tenant := range tenants {
				if tenant.Sites == nil {
					tenant.Sites = domain.Sites{}
				}
				if tenant.Sites["default"] == nil { // null in case of composite. ticket PSUPCLFRM-3603
					tenant.Sites["default"] = make(map[string]domain.AddressList, 0)
				}
				tenant.Sites["default"][serviceName] = append(tenant.Sites["default"][serviceName], domain.Address(host))
				if _, err = tx.NewUpdate().Model(&tenant).WherePK().Exec(ctx); err != nil {
					return wrappers.ErrorWrapper{StatusCode: http.StatusInternalServerError, Message: err.Error()}
				}
			}

			return nil
		})
		return nil, err
	})
	return err
}

func (v *RouteManagerDao) DeleteRouteFromTenants(ctx context.Context, serviceName string) error {
	logger.InfoC(ctx, "Update tenants with service %s", serviceName)

	_, err := v.usingDB(ctx, func(db *bun.DB) (interface{}, error) {
		err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			var tenants []domain.TenantDns
			err := tx.NewSelect().Model(&tenants).Scan(ctx)
			if err != nil {
				return err
			}
			for _, tenant := range tenants {
				for siteName, _ := range tenant.Sites {
					delete(tenant.Sites[siteName], serviceName)
					if _, err = tx.NewUpdate().Model(&tenant).WherePK().Exec(ctx); err != nil {
						return wrappers.ErrorWrapper{StatusCode: http.StatusInternalServerError, Message: err.Error()}
					}
				}
			}
			return nil
		})
		return nil, err
	})
	return err
}

// returns set of all hosts presented in database
func (v *RouteManagerDao) FindAllHosts(ctx context.Context) (*utils.Set, error) {
	if all, err := v.FindAll(ctx); err == nil {
		return flattenHosts(all), nil
	} else {
		logger.ErrorC(ctx, "Error occurred while getting routes from database: %s", err)
		return new(utils.Set), err
	}
}

func flattenHosts(all *[]domain.TenantDns) *utils.Set {
	result := make(utils.Set)
	for _, t := range *all {
		for _, site := range t.Sites {
			for _, services := range site {
				for _, addr := range services {
					result.Put(addr.Host())
				}
			}
		}
	}

	return &result
}
