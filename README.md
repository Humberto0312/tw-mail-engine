# tw-mail-engine

Motor **propio** de envío de email (MTA) de Twilbox. Es el **nodo de entrega**: recibe
órdenes de `api-matrix` y entrega el correo directo a Gmail/Outlook/etc. por el
**puerto 25**, desde IP dedicada con rDNS. Reemplaza la dependencia de proveedores
externos (Alibaba/SendGrid) para **Email Marketing**.

> Las **Notificaciones (transaccional)** siguen por Alibaba, sistema aparte. Este
> motor es solo para Marketing.

## Dónde encaja (las 3 piezas)

```
┌──────────── RAILWAY ────────────┐     ┌──── VPS OVH (mta1.twilbox.com) ────┐
│  api-matrix (Node) = CEREBRO    │     │  tw-mail-engine (Go) = ESTE REPO   │
│  campañas, listas, scheduler,   │ ──▶ │  entrega SMTP:25, DKIM, IP pools,  │
│  tracking, verificación dominio │HTTP │  rebotes, quejas, warm-up          │
└─────────────────────────────────┘     │  deploy: COOLIFY (git push)        │
            ▲                            └────────────────────────────────────┘
            │ tracking aperturas/clics
┌──────────── VERCEL ─────────────┐
│  tw-business (Next.js) = PANEL  │  wizard de verificación de dominio
└─────────────────────────────────┘
```

- **api-matrix (Railway)** = el cerebro. Ya existe (módulo EmailMkt). Llama a este motor.
- **tw-mail-engine (VPS, este repo)** = la entrega física. Lo nuevo.
- **tw-business (Vercel)** = el panel donde la empresa verifica su dominio.

## Responsabilidades del motor

1. **API HTTP** autenticada (Bearer token) que `api-matrix` consume: `POST /v1/send`.
2. **Verificación** de que el `fromEmail` pertenece a un dominio verificado del tenant.
3. **Firma DKIM** con la llave privada del dominio de la empresa (su identidad).
4. **Selección de IP** del pool (warm-up por IP). Diseñado para IP dedicada por cliente.
5. **Entrega SMTP** por puerto 25 (lookup MX + STARTTLS).
6. **Rebotes/quejas**: clasifica hard/soft, reintenta soft, suprime hard, reporta al
   webhook de `api-matrix`.
7. **Límites y warm-up** por IP y por tenant (los candados anti-abuso de la IP compartida).

## Multi-tenant y reputación

- Cada empresa **firma con su propio dominio** (DKIM) → su reputación de dominio es suya.
- **Pools de IP**: empresas buenas en la IP compartida "buena"; nuevas/dudosas en una IP
  de cuarentena/warm-up; empresas con IP dedicada salen por su propia IP (reputación aislada).
- **Candados**: solo dominios verificados, límites por tenant, supresión por rebote/queja,
  pausa automática si sube la tasa de quejas.

## Estructura

```
cmd/engine/main.go        arranque
internal/config/          config por env (.env / Coolify)
internal/core/            logger, mongo, ratelimit (compartido con sync-engine)
internal/api/             server HTTP, auth, /health, /v1/send
```

Pendiente de implementar (siguientes módulos): `sender/` (SMTP+MX), `dkim/` (firma),
`ippool/` (selección+warm-up), `queue/` (cola+workers), `domain/` (verificación DNS),
`bounce/` (clasificación+webhook).

## Configuración

Ver `.env.example`. Claves obligatorias: `MONGO_URI`, `ENGINE_API_TOKEN`.

## Deploy

VPS OVH `mta1.twilbox.com` (IP `149.56.108.135`, puerto 25 saliente confirmado abierto)
vía **Coolify** (`git push` → build con `Dockerfile` → deploy). rDNS de la IP y registros
SPF/DKIM/DMARC se configuran fuera del repo (panel OVH + DNS de cada dominio).

## Estado

v0.1.0 — esqueleto: config, core, API con `/health` y `/v1/send` (contrato definido,
pipeline de entrega en construcción).
