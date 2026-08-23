# Internal HTTP route inventory

All routes below are service-network routes. They are not public gateway routes. Unless noted otherwise, `X-Internal-Token` is required; tenant-scoped routes additionally require trusted `X-Tenant-ID` and `X-App-Type` and fail closed when absent.

| Service | Registered route families | Token | Tenant context | Additional request data |
| --- | --- | --- | --- | --- |
| tenant | `GET /internal/v1/domains/{resolve,tls-authorize}` | yes | no | hostname query; TLS also accepts Caddy's domain query |
| tenant | `GET /internal/v1/{public/config,settings}`, `GET/PATCH /internal/v1/admin/settings` | yes | yes | admin routes additionally require an admin session |
| tenant | `POST/GET /internal/v1/platform/tenants...` | gateway platform credential translates to internal token | no | tenant/domain IDs and JSON bodies according to operation |
| auth | `POST /internal/v1/auth/{login,logout,register,change-password,forgot-password}`, `GET /internal/v1/auth/{me,members...}`, `PATCH /internal/v1/auth/members/:id/role` | yes | yes (`admin`) | session and OWNER role where declared by the route |
| barber | `GET /internal/v1/{barbers/:id/exists,barbers/:id/scheduling-access,branches/:branch_id/barbers/:barber_id/booking-access,public/...}`, `GET/POST/PATCH/DELETE /internal/v1/admin/...` | yes | yes | resource IDs; admin session for admin operations |
| catalog | `GET /internal/v1/{public/...,barbers/:barber_id/services/:service_id/availability}`, `GET/POST/PATCH/DELETE /internal/v1/admin/...` | yes | yes | resource IDs; admin session for admin operations |
| scheduling | `GET /internal/v1/{public/availability,admin/availability}`, `GET/PUT/POST/PATCH/DELETE /internal/v1/admin/barbers/:barber_id/...` | yes | yes | availability query or resource IDs; admin session for admin operations |
| appointment | `GET /internal/v1/{occupancy,appointments/:id/notification-recipient,public/...,customer/...,admin/...}`, `POST /internal/v1/{public/...,customer/...,admin/...}` | yes | yes | query/resource IDs; customer identity or admin session according to route |
| customer | `GET/POST/PATCH /internal/v1/public/customer/...`, `POST /internal/v1/customer/resolve`, `GET /internal/v1/customer/:id`, `GET /internal/v1/internal/customer/session` | yes | yes (`booking`) | customer session/contact/body according to route |
| notification | no HTTP handler routes; it is a JetStream consumer | n/a | event tenant ID is validated by its persistence boundary | n/a |

Each service handler's `internal_auth_contract_test.go` contains the exact method/path manifest. The shared harness compares that manifest with Fiber's registered `/internal/...` routes, so adding an untested route fails `Backend / Tests`. It exercises missing and invalid token rejection plus valid-token admission with absent and malformed tenant context. Tenant control-plane routes additionally prove valid-token admission by reaching input validation. Gateway tests prove that `/internal/...` paths are not registered as public proxy routes.

Current stable boundary statuses are intentionally service-specific: tenant/auth/customer public handlers generally use 401 or 403; barber/catalog use 401 for reads and 403 for writes; scheduling uses 401 for availability and 403 for admin resources; appointment uses 400 for malformed occupancy context, 401 for booking/customer paths, and 403 for admin paths. Bodies are bounded and checked not to disclose presented or configured internal tokens.
