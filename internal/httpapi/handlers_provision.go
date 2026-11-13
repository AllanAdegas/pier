package httpapi

import (
	"net/http"

	"pier/internal/provision"
)

// Cloud provisioning. Registering a cloud token and destroying a machine are
// session-only: an API token (CI, MCP) can list and create machines, never
// add credentials nor destroy anything.
func (a *API) registerProvisionRoutes(mux *http.ServeMux) {
	if a.Provisioning == nil {
		return
	}
	a.route(mux, "GET /api/v1/cloud/credentials", canRead, a.listCloudCredentials)
	a.route(mux, "POST /api/v1/cloud/credentials", sessionOnly, a.createCloudCredential)
	a.route(mux, "DELETE /api/v1/cloud/credentials/{id}", sessionOnly, a.deleteCloudCredential)
	a.route(mux, "GET /api/v1/cloud/credentials/{id}/catalog", canRead, a.cloudCatalog)
	a.route(mux, "GET /api/v1/cloud/machines", canRead, a.listCloudMachines)
	a.route(mux, "POST /api/v1/cloud/machines", canWrite, a.createCloudMachine)
	a.route(mux, "GET /api/v1/cloud/machines/{id}", canRead, a.cloudMachine)
	a.route(mux, "DELETE /api/v1/cloud/machines/{id}", sessionOnly, a.destroyCloudMachine)
}

func (a *API) listCloudCredentials(w http.ResponseWriter, r *http.Request) error {
	credentials, err := a.Provisioning.Credentials(r.Context())
	if err != nil {
		return err
	}
	return ok(w, credentials)
}

func (a *API) createCloudCredential(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[provision.CredentialInput](w, r)
	if err != nil {
		return err
	}
	credential, err := a.Provisioning.CreateCredential(r.Context(), input)
	if err != nil {
		return err
	}
	return created(w, credential)
}

func (a *API) deleteCloudCredential(w http.ResponseWriter, r *http.Request) error {
	if err := a.Provisioning.DeleteCredential(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	return noContent(w)
}

func (a *API) cloudCatalog(w http.ResponseWriter, r *http.Request) error {
	catalog, err := a.Provisioning.Catalog(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, catalog)
}

func (a *API) listCloudMachines(w http.ResponseWriter, r *http.Request) error {
	machines, err := a.Provisioning.Machines(r.Context())
	if err != nil {
		return err
	}
	return ok(w, machines)
}

func (a *API) createCloudMachine(w http.ResponseWriter, r *http.Request) error {
	input, err := decode[provision.MachineInput](w, r)
	if err != nil {
		return err
	}
	machine, err := a.Provisioning.CreateMachine(r.Context(), input)
	if err != nil {
		return err
	}
	return accepted(w, machine)
}

func (a *API) cloudMachine(w http.ResponseWriter, r *http.Request) error {
	machine, err := a.Provisioning.Machine(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return ok(w, machine)
}

// destroyCloudMachine deletes the machine at the provider; the name must be
// typed in ?confirmation=.
func (a *API) destroyCloudMachine(w http.ResponseWriter, r *http.Request) error {
	if err := a.Provisioning.Destroy(r.Context(), r.PathValue("id"), r.URL.Query().Get("confirmation")); err != nil {
		return err
	}
	return noContent(w)
}
