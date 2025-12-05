# Kubernetes Deployment

Deploy SeedReap on Kubernetes using Helm or raw manifests.

## Helm (bjw-s app-template)

Example using the [bjw-s app-template](https://github.com/bjw-s/helm-charts):

```yaml title="helmrelease.yaml"
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: seedreap
spec:
  interval: 30m
  chartRef:
    kind: OCIRepository
    name: app-template
    namespace: default
  values:
    controllers:
      main:
        containers:
          app:
            image:
              repository: ghcr.io/seedreap/seedreap
              tag: latest
            args: []
            probes:
              liveness:
                enabled: true
                custom: true
                spec:
                  httpGet:
                    path: /api/health
                    port: 8423
                  initialDelaySeconds: 10
                  periodSeconds: 30
              readiness:
                enabled: true
                custom: true
                spec:
                  httpGet:
                    path: /api/health
                    port: 8423
                  initialDelaySeconds: 5
                  periodSeconds: 10

    service:
      main:
        controller: main
        ports:
          http:
            port: 8423

    ingress:
      main:
        enabled: true
        className: nginx
        hosts:
          - host: seedreap.example.com
            paths:
              - path: /
                service:
                  identifier: main
                  port: http

    persistence:
      config:
        type: secret
        name: seedreap-config
        globalMounts:
          - path: /config/config.yaml
            subPath: config.yaml
      ssh:
        type: secret
        name: seedreap-ssh
        defaultMode: 0600
        globalMounts:
          - path: /config/ssh
      downloads:
        existingClaim: downloads-pvc
        globalMounts:
          - path: /downloads
```

## Secrets

### Configuration Secret

```yaml title="secret-config.yaml"
apiVersion: v1
kind: Secret
metadata:
  name: seedreap-config
type: Opaque
stringData:
  config.yaml: |
    server:
      listen: "[::]:8423"
    sync:
      downloads_path: /downloads
      syncing_path: /downloads/syncing
      max_concurrent: 2
      parallel_connections: 8
    downloaders:
      seedbox:
        type: qbittorrent
        url: http://seedbox:8080
        username: admin
        password: your-password
        ssh:
          host: seedbox
          port: 22
          user: user
          key_file: /config/ssh/id_ed25519
    apps:
      sonarr:
        type: sonarr
        url: http://sonarr:8989
        api_key: your-api-key
        category: tv-sonarr
```

### SSH Secret

```yaml title="secret-ssh.yaml"
apiVersion: v1
kind: Secret
metadata:
  name: seedreap-ssh
type: Opaque
data:
  id_ed25519: <base64-encoded-private-key>
```

Generate the base64 value:

```bash
cat ~/.ssh/seedbox_key | base64 -w0
```

!!! warning "Secret Management"
    Consider using a secrets management solution like:
    - [External Secrets Operator](https://external-secrets.io/)
    - [Sealed Secrets](https://sealed-secrets.netlify.app/)
    - [SOPS](https://github.com/getsops/sops)

## Raw Manifests

### Deployment

```yaml title="deployment.yaml"
apiVersion: apps/v1
kind: Deployment
metadata:
  name: seedreap
spec:
  replicas: 1
  selector:
    matchLabels:
      app: seedreap
  template:
    metadata:
      labels:
        app: seedreap
    spec:
      containers:
        - name: seedreap
          image: ghcr.io/seedreap/seedreap:latest
          args: []
          ports:
            - containerPort: 8423
          volumeMounts:
            - name: config
              mountPath: /config/config.yaml
              subPath: config.yaml
            - name: ssh
              mountPath: /config/ssh
            - name: downloads
              mountPath: /downloads
          livenessProbe:
            httpGet:
              path: /api/health
              port: 8423
            initialDelaySeconds: 10
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /api/health
              port: 8423
            initialDelaySeconds: 5
            periodSeconds: 10
      volumes:
        - name: config
          secret:
            secretName: seedreap-config
        - name: ssh
          secret:
            secretName: seedreap-ssh
            defaultMode: 0600
        - name: downloads
          persistentVolumeClaim:
            claimName: downloads-pvc
```

### Service

```yaml title="service.yaml"
apiVersion: v1
kind: Service
metadata:
  name: seedreap
spec:
  selector:
    app: seedreap
  ports:
    - port: 8423
      targetPort: 8423
```

## Storage

SeedReap needs access to:

1. A staging directory (`syncing_path`)
2. The final downloads directory (`downloads_path`)

These should be on the same filesystem for atomic moves. Use a PVC that your
media apps also have access to:

```yaml title="pvc.yaml"
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: downloads-pvc
spec:
  accessModes:
    - ReadWriteMany  # If multiple pods need access
  resources:
    requests:
      storage: 1Ti
  storageClassName: your-storage-class
```
