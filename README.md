# TicketHN API

API REST para TicketHN, una plataforma universitaria de venta y validación de boletos digitales. Incluye autenticación, gestión de sesiones, eventos, cartelera, compras simuladas y consulta de boletos.

## Tecnologías

Go 1.26.3, `net/http`, `chi`, PostgreSQL alojado en Supabase, `pgxpool` de pgx v5, `log/slog` y `testing` con `httptest`. La autenticación usa bcrypt y JWT HS256. Los correos se envían mediante la API HTTP de Resend. La configuración local se puede cargar desde `.env` con `godotenv`.

## Arquitectura

- `cmd/api` conecta las dependencias y administra el servidor HTTP y su apagado.
- `internal/domain` define usuarios, roles, sesiones, eventos, localidades, órdenes y boletos.
- `internal/application` contiene los casos de uso y sus contratos de persistencia, almacenamiento de afiches y firma de tokens y QR.
- `internal/infrastructure` implementa PostgreSQL, Supabase Storage, bcrypt, JWT, firma de QR y Resend.
- `internal/presentation` registra las rutas con `chi`, aplica los límites de intentos y contiene los handlers y el middleware JWT y de roles.

```text
api_tickethn_grupo3_final_project/
├── cmd/api/
├── internal/
│   ├── application/{auth,events,health}/
│   ├── domain/
│   ├── infrastructure/
│   │   ├── config/
│   │   ├── database/
│   │   ├── email/
│   │   ├── security/
│   │   └── storage/
│   └── presentation/
│       ├── handlers/
│       ├── middleware/
│       ├── response/
│       └── router/
├── .env.example
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```

## Ejecutar en local

Necesitas acceso a PostgreSQL en Supabase con las tablas de autenticación y las tablas existentes `public.events`, `public.ticket_types`, `public.orders` y `public.tickets`, además de un remitente habilitado en Resend. Ejecuta los comandos desde la carpeta de la API.

```sh
test -f .env || cp .env.example .env
```

| Variable | Configuración |
| --- | --- |
| `APP_ENV` | `development` por defecto. En producción configura `APP_ENV=production` para leer las variables del sistema. |
| `PORT` | Puerto HTTP. El valor por defecto es `8080`. |
| `DATABASE_URL` | Obligatoria. Copia la cadena PostgreSQL del panel **Connect** de Supabase, reemplaza el marcador por tu contraseña codificada para URL y conserva `sslmode=require` o la configuración TLS de tu proyecto. |
| `JWT_SECRET` | Obligatoria. Usa un secreto aleatorio de al menos 32 bytes. Genera uno con `openssl rand -hex 32` y guárdalo en tu configuración privada. |
| `JWT_ISSUER` | Emisor del access token. Predeterminado `tickethn-api`. |
| `JWT_ACCESS_TTL_MINUTES` | Vigencia del access token. Predeterminado `30`. |
| `JWT_REFRESH_TTL_DAYS` | Vigencia de la sesión desde el login. Predeterminado `7`. |
| `RESEND_API_KEY` | Clave de envío de Resend. |
| `EMAIL_FROM` | Remitente autorizado en Resend, por ejemplo `TicketHN <no-reply@tu-dominio>`. |
| `SUPABASE_URL` | Origen HTTPS del proyecto para Storage, por ejemplo `https://<project-ref>.supabase.co`. |
| `SUPABASE_SERVICE_ROLE_KEY` | Clave de servidor `service_role` de Supabase, de la sección de claves JWT legacy. |
| `SUPABASE_EVENT_COVERS_BUCKET` | Nombre del bucket público de afiches. Por defecto, `event-covers`. |
| `TICKET_QR_SECRET` | Secreto aleatorio de al menos 32 bytes, generado independientemente de `JWT_SECRET`. Por ejemplo, ejecuta `openssl rand -hex 32`. |

Puedes usar la conexión directa si tu red llega al host de Supabase. En una red solo IPv4, usa **Session pooler** en el puerto `5432`. [Guía de conexión de Supabase](https://supabase.com/docs/guides/database/connecting-to-postgres)

Guarda las variables locales en `.env`. Las variables del sistema tienen prioridad. Configura las variables de Storage para adjuntar afiches y el secreto de QR para consultar el detalle de un boleto. Cada función responde `503` si le falta su configuración.

### Ejecutar con Go

Si `go version` indica que el comando no existe o muestra una versión anterior a **Go 1.26.3**, instala una versión compatible para tu sistema y arquitectura. Abre una terminal nueva y ejecuta `go version` para verificar la instalación. Si el comando sigue sin aparecer, comprueba que la carpeta `bin` de Go esté en tu `PATH`. [Instalación oficial de Go](https://go.dev/doc/install)

```sh
go mod download
go run ./cmd/api
```

### Ejecutar con Docker sin instalar Go

Con Docker en ejecución, usa el `Dockerfile` del repositorio. Para `docker run --env-file`, escribe los valores de `.env` como `CLAVE=valor`, sin comillas externas ni comentarios al final de la línea. Por ejemplo, `EMAIL_FROM=TicketHN <tickethn@kongrega.app>` si ese es tu remitente autorizado. [Variables de entorno en Docker](https://docs.docker.com/reference/cli/docker/container/run/#env)

```sh
docker build -t tickethn-api .
docker run --rm --env-file .env -e PORT=8080 -p 127.0.0.1:8080:8080 tickethn-api
```

Para desplegar desde GitHub, Cloud Build compila la imagen con el `Dockerfile`. Puedes seguir la [guía de Cloud Run](#desplegar-en-cloud-run-desde-github) sin instalar Go en tu equipo.

El servidor empieza a escuchar después de un `Ping` inicial a PostgreSQL. Si la comprobación falla, el proceso termina con un error. Usa `Ctrl+C` para iniciar el apagado ordenado.

## Health check

```sh
curl -i http://localhost:8080/api/health
```

Cada petición ejecuta un `Ping` con un límite de dos segundos. Las respuestas usan `Content-Type: application/json`.

**HTTP 200 OK**

```json
{
  "status": "healthy",
  "service": "TicketHN API",
  "database": "connected"
}
```

**HTTP 503 Service Unavailable**

```json
{
  "status": "unhealthy",
  "service": "TicketHN API",
  "database": "disconnected"
}
```

## Compilación y pruebas

```sh
go build ./...
go test ./...
```

Las pruebas unitarias cubren el health check, los flujos de autenticación, los JWT, los roles y los errores de Resend con implementaciones falsas.

Las pruebas de eventos simulan errores de Storage y PostgreSQL. Para ejecutar también las pruebas de integración, inicia una base PostgreSQL desechable. El test crea y limpia sus propias tablas en una base local vacía.

```sh
docker run --rm -d --name tickethn-events-test -e POSTGRES_PASSWORD=tickethn_test \
  -e POSTGRES_DB=tickethn_events_test -p 127.0.0.1:55432:5432 postgres:17
docker exec tickethn-events-test pg_isready -U postgres -d tickethn_events_test
TICKETHN_TEST_DATABASE_URL='postgresql://postgres:tickethn_test@127.0.0.1:55432/tickethn_events_test?sslmode=disable' go test ./...
docker stop tickethn-events-test
```

Ejecuta las pruebas cuando `pg_isready` indique que PostgreSQL acepta conexiones. La integración verifica la compra concurrente del último boleto, la reversión de cupos, la publicación y la propiedad de órdenes y boletos.

## Eventos, cartelera y boletos

| Método y ruta | Acceso | Resultado |
| --- | --- | --- |
| `POST /api/organizer/events` | ORGANIZADOR | `201` con el borrador creado desde multipart. |
| `GET /api/organizer/events` | ORGANIZADOR | `200` con los eventos propios, incluidos borradores y sus localidades. |
| `GET /api/organizer/events/{id}` | ORGANIZADOR | `200` con el evento propio y sus localidades. |
| `PATCH /api/organizer/events/{id}` | ORGANIZADOR | `200` con el evento actualizado desde JSON parcial. |
| `PUT /api/organizer/events/{id}/cover` | ORGANIZADOR | `200` con el evento actualizado. El multipart requiere `cover`. |
| `POST /api/organizer/events/{id}/ticket-types` | ORGANIZADOR | `201` con la localidad creada desde JSON. |
| `PATCH /api/organizer/events/{id}/ticket-types/{ticketTypeId}` | ORGANIZADOR | `200` con la localidad actualizada desde JSON parcial. |
| `POST /api/organizer/events/{id}/publish` | ORGANIZADOR | `200` con el evento publicado. |
| `GET /api/events` | Público | `200` con eventos publicados y próximos, ordenados por `starts_at` e `id`. |
| `GET /api/events/{id}` | Público | `200` con un evento publicado y próximo. |
| `POST /api/orders` | COMPRADOR | `201` con la orden confirmada y sus boletos. |
| `GET /api/orders` | COMPRADOR | `200` con las órdenes propias. |
| `GET /api/tickets` | COMPRADOR | `200` con los boletos propios. |
| `GET /api/tickets/{id}` | COMPRADOR | `200` con el boleto propio y su `qr_payload`. |

Las rutas protegidas usan `Authorization: Bearer <access_token>`. Los identificadores son UUID. Los listados devuelven arreglos y aceptan `limit` (1 a 100, por defecto 20) y `offset` (entero no negativo, por defecto 0). Los listados privados se ordenan por creación e ID descendentes.

Para crear un borrador solo se requiere `title`. Envía `starts_at` en RFC 3339 con zona explícita y `latitude` y `longitude` juntas, si las usas. En un PATCH puedes enviar `null` para borrar un campo opcional. Publicar requiere descripción, dirección, inicio futuro y una localidad activa. Una vez emitidos boletos se bloquean los cambios de fecha, dirección y coordenadas. La cartelera incluye `cover_image_url` y solo localidades activas con `available = capacity - sold_count`.

Una localidad se crea con `title`, `price` (número en HNL con hasta dos decimales), `capacity` (entero positivo), `description` opcional e `is_active` (por defecto `true`). Un evento publicado conserva al menos una localidad activa. El cupo debe cubrir los boletos ya vendidos.

Para comprar, envía `{"event_id":"<uuid>","items":[{"ticket_type_id":"<uuid>","quantity":2}]}`. El máximo es de **20 entradas en total por orden** y cada localidad aparece una sola vez. PostgreSQL aporta precios y genera los códigos de los boletos. La compra confirma todos los boletos en una transacción y revierte todos los cambios si una localidad carece de cupo. Android puede conservar el `qr_payload` para mostrarlo sin conexión. Contiene los identificadores firmados del boleto y el evento. El código legible es un folio. La validación en puerta corresponde a otra etapa.

Los errores mantienen el formato `{"error":{"code":"…","message":"…"}}`. Los campos inválidos o desconocidos devuelven `400 INVALID_INPUT`. Las rutas protegidas devuelven `401 UNAUTHORIZED` por un JWT inválido y `403 FORBIDDEN` por falta de rol. Un recurso ajeno, inexistente o ausente de la cartelera devuelve `404 NOT_FOUND`. Los conflictos de estado y de cupo devuelven `409 EVENT_CONFLICT` o `409 TICKET_STOCK`. Para afiches, `413 BODY_TOO_LARGE` indica exceso de tamaño y `415 INVALID_COVER` indica imagen inválida, formato incompatible o dimensiones fuera del límite. `503 STORAGE_UNAVAILABLE` indica un fallo de subida o falta de configuración de Storage.

### Configurar y subir afiches

En **Supabase → Storage**, crea el bucket público `event-covers` o el nombre configurado en `SUPABASE_EVENT_COVERS_BUCKET`. Limita cada objeto a **5 MiB (5 242 880 bytes)** y permite `image/jpeg`, `image/png` e `image/webp`. Mantén las operaciones de escritura y borrado reservadas al backend mediante la clave `service_role`. Guarda esa clave exclusivamente en el servidor. La lectura pública por URL también aplica a los afiches de borradores, que la cartelera excluye. [Control de acceso de Supabase Storage](https://supabase.com/docs/guides/storage/security/access-control)

Go acepta un solo afiche y comprueba el formato por sus bytes, con un máximo de 4 096 píxeles por lado y 12 millones de píxeles en total. El multipart completo admite 5 MiB más 64 KiB. Si se omite `cover` al crear un evento, `cover_image_url` es `null`. Si se reemplaza un afiche, el objeto anterior del bucket se elimina tras confirmar el guardado y verificar que ningún evento lo referencia. Los errores de limpieza quedan en el log.

```sh
curl -i http://localhost:8080/api/organizer/events \
  -H "Authorization: Bearer $ORGANIZER_TOKEN" \
  -F 'title=Concierto de TicketHN' \
  -F 'description=Música en vivo' \
  -F 'address=Tegucigalpa' \
  -F 'starts_at=2030-06-15T19:00:00-06:00' \
  -F 'cover=@afiche.jpg'
```

## Probar autenticación con Postman

Con el servidor activo, importa `http://localhost:8080/api/openapi.yaml` en Postman. También puedes importar el archivo `internal/presentation/router/openapi.yaml` desde un editor Swagger o desde Postman. Si cambiaste `PORT`, ajusta la URL base de la colección. Usa `Content-Type: application/json` en las peticiones POST.

| Paso | Método y ruta | Datos o resultado esperado |
| --- | --- | --- |
| 1 | `GET /api/health` | HTTP 200 con PostgreSQL conectado. |
| 2 | `POST /api/auth/register` | Envía `full_name`, `email` y `password`. Usa un buzón que controles. HTTP 201. |
| 3 | `POST /api/auth/verify-email` | Envía `email` y `code` del correo. Conserva los ceros iniciales. HTTP 200. |
| 4 | `POST /api/auth/login` | Envía `email` y `password`. Guarda `access_token` y `refresh_token`. |
| 5 | `GET /api/auth/me` | Envía `Authorization: Bearer <access_token>`. Comprueba el rol `COMPRADOR`. |
| 6 | `GET /api/auth/sessions` | Envía el Bearer y guarda el ID de la sesión que quieras administrar. |
| 7 | `POST /api/auth/refresh` | Envía `refresh_token` en el JSON. Reemplaza ambos tokens con los recibidos. Repetir el refresh anterior devuelve HTTP 401. |
| 8 | `POST /api/auth/logout` | Envía el Bearer y el refresh actual. Después ese refresh devuelve HTTP 401. |
| 9 | `DELETE /api/auth/sessions/{id}` | Como alternativa al cierre de sesión, usa el Bearer y el ID de una sesión propia. |
| 10 | `POST /api/auth/forgot-password` | Envía `email`. El mensaje de respuesta es genérico. |
| 11 | `POST /api/auth/reset-password` | Envía `email`, `code` y `new_password`. Prueba iniciar sesión con la nueva contraseña. Los refresh anteriores quedan revocados. |

Si no llega el correo de verificación, espera al menos 60 segundos desde la última emisión y usa `POST /api/auth/resend-verification` con `email`. Revisa el estado de envío en Resend y los logs del servidor. Los envíos se procesan en una cola en memoria, por lo que un HTTP 201 o 200 confirma la operación de la API y la entrega se comprueba en Resend.

## Administración de accesos

Las rutas requieren `Authorization: Bearer <access_token>` de un usuario activo, verificado y con el rol `ADMIN`.

| Método y ruta | Resultado |
| --- | --- |
| `GET /api/admin/users` | Devuelve los ID, nombres, correos y roles de todos los usuarios. |
| `PUT /api/admin/users/{id}/roles/{role}` | Asigna el rol al usuario indicado y devuelve `204`. |
| `DELETE /api/admin/users/{id}/roles/{role}` | Retira el rol del usuario indicado y devuelve `204`. |

`id` es el UUID del usuario y `role` acepta `COMPRADOR`, `ORGANIZADOR`, `VALIDADOR` o `ADMIN`. Si aún no hay una cuenta con `ADMIN`, la primera asignación se hace en la tabla `public.user_roles` de Supabase, vinculando el ID de una cuenta activa y verificada con el ID del rol `ADMIN` de `public.roles`.

## Seguridad y sesiones

- Las contraseñas nuevas requieren al menos 12 caracteres y un máximo de 72 bytes UTF-8. Los códigos de seis dígitos vencen en 10 minutos y solo se pueden consumir una vez.
- Los límites de peticiones por correo e IP se guardan en memoria por instancia. La base aplica además un mínimo de 60 segundos entre emisiones de un código del mismo tipo para una cuenta. Si se ejecutan varias instancias, los contadores de intentos requieren un almacenamiento compartido para aplicar un único límite global. La IP registrada viene de la conexión HTTP, incluida la IP del proxy si se usa uno.
- El login devuelve el mismo error para credenciales incorrectas, cuentas inactivas o correos pendientes. Los intentos que llegan a la validación de credenciales se auditan en `login_events`. Las peticiones rechazadas por JSON inválido o por el límite HTTP se rechazan antes de esa validación.
- La rotación conserva el ID y la expiración original de la sesión. El refresh previo deja de servir inmediatamente. Cerrar sesión, revocar una sesión o restablecer la contraseña invalida los refresh afectados. Los access tokens ya emitidos pueden seguir autenticando hasta expirar mientras la cuenta esté activa y verificada.
- El middleware lee los roles actuales de PostgreSQL. Al agregar una ruta que requiera permisos, aplica el middleware JWT seguido de `RequireRoles(domain.Admin, domain.Organizer)` para permitir cualquiera de esos roles.

## Esquema y migración opcional

El módulo usa el esquema existente de Supabase y el trigger de registro para asignar `COMPRADOR`. La rotación atómica funciona con `user_sessions.refresh_token_hash` y rechaza la reutilización con HTTP 401. Para distinguir en auditoría un token ya rotado de uno desconocido, sería necesario conservar el hash anterior al actualizar la sesión. Propuesta para una fase que requiera esa distinción:

```sql
CREATE TABLE public.used_refresh_tokens (
    token_hash text PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES public.user_sessions (id) ON DELETE CASCADE,
    rotated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE public.used_refresh_tokens ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON public.used_refresh_tokens FROM anon, authenticated;
```

La adopción requiere insertar el hash previo dentro de la misma transacción que rota la sesión, consultar este historial al fallar una búsqueda y purgar las entradas de sesiones expiradas según la política de retención. La migración es una propuesta y no se ha aplicado.

## Desplegar en Cloud Run desde GitHub

1. Sube el código y el `Dockerfile` a la rama de GitHub que vas a desplegar. En Google Cloud Console, abre **Cloud Run → Conectar repositorio**, elige **Cloud Build** y selecciona el repositorio y la rama. En **Tipo de compilación**, selecciona **Dockerfile**. En **Ubicación de origen**, usa `Dockerfile` si el repositorio de GitHub empieza en la carpeta de la API, o `api_tickethn_grupo3_final_project/Dockerfile` si el repositorio incluye la carpeta del proyecto completo. [Despliegue continuo desde un repositorio](https://docs.cloud.google.com/run/docs/continuous-deployment)
2. En **Contenedores → Variables y secretos → Añadir variable**, agrega estos valores. Escribe el remitente en el campo de valor sin comillas externas. [Variables de entorno en Cloud Run](https://docs.cloud.google.com/run/docs/configuring/services/environment-variables)

   | Variable | Valor |
   | --- | --- |
   | `EMAIL_FROM` | `TicketHN <tickethn@kongrega.app>` si es el remitente que ya probaste en Resend. |
   | `APP_ENV` | `production`. El contenedor ya trae este valor por defecto. |

   `JWT_ISSUER`, `JWT_ACCESS_TTL_MINUTES` y `JWT_REFRESH_TTL_DAYS` son opcionales. La API usa `tickethn-api`, `30` y `7` por defecto. Deja el puerto del contenedor en `8080`. Cloud Run inyecta `PORT`.

3. En **Secret Manager**, crea los tres secretos y concede a la cuenta de servicio de ejecución el rol **Secret Manager Secret Accessor** sobre cada uno. En **Variables y secretos → Referenciar un secreto**, elige una versión y expón cada secreto como variable de entorno con el nombre que espera la API. [Secretos en Cloud Run](https://docs.cloud.google.com/run/docs/configuring/services/secrets)

   | Variable | Valor del secreto |
   | --- | --- |
   | `DATABASE_URL` | URI de **Session pooler** de Supabase, puerto `5432`, con la contraseña codificada para URL y `sslmode=require`. |
   | `JWT_SECRET` | Secreto aleatorio de al menos 32 bytes para Cloud Run. |
   | `RESEND_API_KEY` | Clave de envío de Resend. |

4. Selecciona **Permitir acceso público** para que la app móvil pueda registrarse e iniciar sesión. Las rutas protegidas siguen validando el JWT de TicketHN. En **Facturación**, selecciona **Basada en instancias** para que Resend pueda procesar la cola después de responder. Se cobra mientras la instancia está activa. Puedes empezar con mínimo `0` y máximo `1` para acotar las conexiones a Supabase. La cola y los límites por IP y correo viven en memoria de la instancia. [Facturación en Cloud Run](https://docs.cloud.google.com/run/docs/configuring/billing-settings)
5. Crea el servicio. Abre `/api/health` en la URL que te dé Cloud Run y comprueba HTTP 200 con `"database":"connected"`. Para probar autenticación, importa `/api/openapi.yaml` desde esa misma URL en Postman y úsala como URL base.
