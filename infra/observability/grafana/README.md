# Dashboards Grafana da VPS

Cópia versionada dos dashboards provisionados em `/srv/infra/observability/grafana`.

| Pasta no repo | Destino na VPS | Provider/folder |
|---|---|---|
| `dashboards-vps/` | `grafana/dashboards-vps/` | `vps` / **VPS** |
| `dashboards-elama/` | `grafana/dashboards-elama/` | `elama` / **Elama** |

Os datasources esperados são `prometheus` (Prometheus), `loki` (Loki) e
`tempo` (Tempo), já provisionados no mesmo stack. Para aplicar, copie os JSONs
para a VPS e aguarde o `updateIntervalSeconds: 30` do provider (ou reinicie o
Grafana).
