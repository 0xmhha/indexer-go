# Operations Guide

Operational handbook for deploying, monitoring, and maintaining indexer-go in production environments.

**Last Updated**: 2025-11-20

---

## Table of Contents

- [Deployment](#deployment)
- [Service Management](#service-management)
- [Monitoring](#monitoring)
- [Troubleshooting](#troubleshooting)
- [Maintenance](#maintenance)
- [Backup & Recovery](#backup--recovery)
- [Performance Tuning](#performance-tuning)
- [Security](#security)

---

## Deployment

### Prerequisites

**System Requirements**:
- Linux (Ubuntu 20.04+, CentOS 8+, or similar)
- 4+ CPU cores
- 8+ GB RAM
- 100+ GB SSD storage
- Network access to Stable-One RPC endpoint

**Software Requirements**:
- systemd
- logrotate
- curl, wget, jq (for health checks)
- Prometheus (optional, for monitoring)
- Grafana (optional, for visualization)

### Installation

#### 1. Automated Deployment

```bash
# Clone repository
git clone https://github.com/0xmhha/indexer-go.git
cd indexer-go/deployments/scripts

# Run deployment script
sudo ./deploy.sh latest
```

#### 2. Manual Deployment

```bash
# Create user
sudo useradd --system --user-group --shell /bin/false \
  --home-dir /var/lib/indexer-go --create-home indexer

# Create directories
sudo mkdir -p /opt/indexer-go/{bin,backup}
sudo mkdir -p /etc/indexer-go
sudo mkdir -p /var/lib/indexer-go
sudo mkdir -p /var/log/indexer-go

# Copy binary
sudo cp build/indexer-go /opt/indexer-go/bin/
sudo chmod 755 /opt/indexer-go/bin/indexer-go

# Copy configuration
sudo cp config.example.yaml /etc/indexer-go/config.yaml

# Install systemd service
sudo cp deployments/systemd/indexer-go.service /etc/systemd/system/
sudo systemctl daemon-reload

# Install logrotate
sudo cp deployments/logrotate/indexer-go /etc/logrotate.d/

# Set permissions
sudo chown -R indexer:indexer /var/lib/indexer-go
sudo chown -R indexer:indexer /var/log/indexer-go
```

### Configuration

#### Edit Configuration Files

```bash
# Main configuration (primary method)
sudo nano /etc/indexer-go/config.yaml
```

**Critical Settings**:
```yaml
rpc:
  endpoint: "http://your-rpc-node:8545"  # REQUIRED

database:
  path: "/var/lib/indexer-go/data"

api:
  enabled: true
  host: "0.0.0.0"  # Listen on all interfaces
  port: 8080
```

---

## Service Management

### Start Service

```bash
# Enable service (start on boot)
sudo systemctl enable indexer-go

# Start service
sudo systemctl start indexer-go

# Check status
sudo systemctl status indexer-go
```

### Stop Service

```bash
# Graceful stop
sudo systemctl stop indexer-go

# Force stop (if graceful fails)
sudo systemctl kill indexer-go
```

### Restart Service

```bash
# Graceful restart
sudo systemctl restart indexer-go

# Reload configuration (without restart)
sudo systemctl reload indexer-go
```

### View Logs

```bash
# Follow logs
sudo journalctl -u indexer-go -f

# View last 100 lines
sudo journalctl -u indexer-go -n 100

# View logs since specific time
sudo journalctl -u indexer-go --since "1 hour ago"

# View logs with filters
sudo journalctl -u indexer-go -p err  # Errors only
```

---

## Monitoring

### Health Checks

#### Manual Health Check

```bash
# Run health check script
./deployments/scripts/health-check.sh localhost:8080

# Or manually check endpoints
curl http://localhost:8080/health | jq .
curl http://localhost:8080/version | jq .
curl http://localhost:8080/metrics
curl http://localhost:8080/subscribers | jq .
```

#### Expected Health Response

```json
{
  "status": "ok",
  "timestamp": "2025-10-20T15:00:00Z",
  "eventbus": {
    "subscribers": 5,
    "total_events": 1000000,
    "total_deliveries": 5000000,
    "dropped_events": 0
  }
}
```

### Prometheus Integration

#### prometheus.yml Configuration

```yaml
scrape_configs:
  - job_name: 'indexer-go'
    scrape_interval: 15s
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
```

#### Key Metrics to Monitor

```promql
# Events per second
rate(indexer_eventbus_events_published_total[5m])

# Subscriber count
indexer_eventbus_subscribers_total

# Drop rate
rate(indexer_eventbus_events_dropped_total[5m])

# p99 latency
histogram_quantile(0.99,
  rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m])
)
```

### Grafana Dashboard

Import the provided dashboard:

```bash
# Upload dashboard JSON
curl -X POST http://grafana:3000/api/dashboards/db \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d @deployments/grafana/dashboard.json
```

### Alerting Rules

#### Example Alert Rules

```yaml
groups:
  - name: indexer-go
    rules:
      # High drop rate
      - alert: HighEventDropRate
        expr: rate(indexer_eventbus_events_dropped_total[5m]) > 100
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High event drop rate detected"

      # Service down
      - alert: IndexerDown
        expr: up{job="indexer-go"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Indexer-Go service is down"

      # High latency
      - alert: HighEventLatency
        expr: histogram_quantile(0.99,
          rate(indexer_eventbus_event_delivery_duration_seconds_bucket[5m])
        ) > 0.01
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Event delivery latency is high"
```

---

## Troubleshooting

### Service Won't Start

**Check logs**:
```bash
sudo journalctl -u indexer-go -n 50
```

**Common Issues**:

1. **Configuration error**
   ```
   Error: invalid configuration: rpc.endpoint is required
   ```
   → Fix: Edit `/etc/indexer-go/config.yaml` and set RPC endpoint

2. **Port already in use**
   ```
   Error: bind: address already in use
   ```
   → Fix: Change port in configuration or stop conflicting service

3. **Permission denied**
   ```
   Error: open /var/lib/indexer-go/data: permission denied
   ```
   → Fix: Check directory permissions
   ```bash
   sudo chown -R indexer:indexer /var/lib/indexer-go
   ```

### High Memory Usage

**Check memory**:
```bash
# Process memory
ps aux | grep indexer-go

# System memory
free -h
```

**Solutions**:
- Reduce `--workers` count
- Reduce EventBus buffer sizes
- Check for memory leaks (unlikely with Go)

### High CPU Usage

**Check CPU**:
```bash
top -p $(pgrep indexer-go)
```

**Solutions**:
- Reduce worker count
- Optimize database (PebbleDB compaction)
- Check for infinite loops in subscribers

### Events Being Dropped

**Check drop rate**:
```bash
curl http://localhost:8080/metrics | grep dropped
```

**Root Causes**:
1. Slow subscribers (can't keep up)
2. Small channel buffers
3. High event rate

**Solutions**:
- Increase subscriber channel sizes
- Optimize subscriber processing
- Use async processing in subscribers
- Increase `subscribe_buffer` size

### Database Issues

**Check database size**:
```bash
du -sh /var/lib/indexer-go/data
```

**Common Issues**:
1. **Disk full**
   → Free up space or move database

2. **Corruption**
   → Restore from backup

3. **Slow queries**
   → Check PebbleDB compaction
   → Use SSD storage

---

## Maintenance

### Regular Maintenance Tasks

#### Daily
- Monitor health endpoints
- Check for dropped events
- Review error logs

#### Weekly
- Verify backup integrity
- Check disk space usage
- Review performance metrics

#### Monthly
- Update to latest version
- Database optimization
- Security updates

### Upgrades

#### Zero-Downtime Upgrade (Rolling Deployment)

If running multiple instances:

```bash
# Upgrade instance 1
sudo systemctl stop indexer-go-1
sudo cp new-binary /opt/indexer-go/bin/indexer-go
sudo systemctl start indexer-go-1

# Verify health
curl http://instance-1:8080/health

# Upgrade instance 2 (repeat)
```

#### Single Instance Upgrade

```bash
# Stop service
sudo systemctl stop indexer-go

# Backup current binary
sudo cp /opt/indexer-go/bin/indexer-go \
  /opt/indexer-go/backup/indexer-go.$(date +%Y%m%d)

# Update binary
sudo cp new-binary /opt/indexer-go/bin/indexer-go
sudo chmod 755 /opt/indexer-go/bin/indexer-go

# Start service
sudo systemctl start indexer-go

# Verify
curl http://localhost:8080/health
```

### Database Maintenance

#### Compact Database

```bash
# Stop service
sudo systemctl stop indexer-go

# Run manual compaction (if supported)
# Note: PebbleDB auto-compacts

# Start service
sudo systemctl start indexer-go
```

#### Vacuum/Optimize

```bash
# Check database stats
du -sh /var/lib/indexer-go/data/*

# Remove old WAL logs (if needed)
# PebbleDB manages this automatically
```

---

## Backup & Recovery

### Backup Strategy

#### Database Backup

```bash
#!/bin/bash
# Daily backup script

BACKUP_DIR="/backup/indexer-go"
DATE=$(date +%Y%m%d)

# Stop service for consistent backup
sudo systemctl stop indexer-go

# Backup database
sudo tar -czf ${BACKUP_DIR}/data-${DATE}.tar.gz \
  -C /var/lib/indexer-go data

# Backup configuration
sudo tar -czf ${BACKUP_DIR}/config-${DATE}.tar.gz \
  -C /etc indexer-go

# Restart service
sudo systemctl start indexer-go

# Remove old backups (keep 30 days)
find ${BACKUP_DIR} -name "*.tar.gz" -mtime +30 -delete
```

#### Hot Backup (with downtime)

For minimal downtime, use snapshot if available:

```bash
# LVM snapshot
sudo lvcreate -L 10G -s -n data-snapshot /dev/vg0/data

# Backup from snapshot
sudo tar -czf backup.tar.gz /mnt/snapshot

# Remove snapshot
sudo lvremove /dev/vg0/data-snapshot
```

### Recovery

#### Restore from Backup

```bash
# Stop service
sudo systemctl stop indexer-go

# Restore database
sudo tar -xzf backup.tar.gz -C /var/lib/indexer-go/

# Set permissions
sudo chown -R indexer:indexer /var/lib/indexer-go

# Start service
sudo systemctl start indexer-go
```

#### Disaster Recovery

If database is corrupted:

```bash
# 1. Stop service
sudo systemctl stop indexer-go

# 2. Move corrupted database
sudo mv /var/lib/indexer-go/data /var/lib/indexer-go/data.corrupt

# 3. Restore from backup OR start fresh
sudo tar -xzf latest-backup.tar.gz -C /var/lib/indexer-go/

# 4. Start service (will sync from RPC)
sudo systemctl start indexer-go
```

---

## Performance Tuning

### Worker Pool Tuning

```yaml
indexer:
  workers: 100  # Default
  # Increase for faster sync (if RPC allows)
  # Decrease if RPC is overloaded
```

**Guidelines**:
- Start with 100 workers
- Monitor RPC node load
- Increase gradually to 200-500 if RPC can handle it
- Decrease if seeing RPC errors

### EventBus Tuning

```yaml
# In code or environment
INDEXER_EVENTBUS_PUBLISH_BUFFER=1000
INDEXER_EVENTBUS_SUBSCRIBE_BUFFER=100
```

**Tuning Guide**:
- **High event rate**: Increase publish buffer (2000-10000)
- **Many subscribers**: Increase subscribe buffer (200-1000)
- **Low latency**: Decrease buffers (100-500)
- **Slow subscribers**: Increase individual channel sizes

### Database Performance

**Use SSD**:
- PebbleDB is I/O intensive
- SSD provides 10-100x improvement

**Filesystem**:
- ext4 or XFS recommended
- noatime mount option

```bash
# /etc/fstab
/dev/sda1 /var/lib/indexer-go ext4 defaults,noatime 0 2
```

### Network Optimization

**TCP Tuning**:
```bash
# /etc/sysctl.conf
net.ipv4.tcp_keepalive_time = 120
net.ipv4.tcp_keepalive_probes = 3
net.ipv4.tcp_keepalive_intvl = 10
```

**Connection Limits**:
```bash
# Increase file descriptor limit
ulimit -n 65536
```

---

## Security

### Network Security

**Firewall Rules**:
```bash
# Allow API port (with restrictions)
sudo ufw allow from 10.0.0.0/8 to any port 8080

# Allow Prometheus scraping
sudo ufw allow from prometheus-server to any port 8080
```

**Reverse Proxy** (recommended):
```nginx
# nginx
server {
    listen 80;
    server_name indexer.example.com;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    # Rate limiting
    limit_req_zone $binary_remote_addr zone=api:10m rate=100r/s;
    limit_req zone=api burst=200 nodelay;
}
```

### Authentication

Add authentication layer using nginx or API gateway:

```nginx
# Basic auth
location /metrics {
    auth_basic "Prometheus Metrics";
    auth_basic_user_file /etc/nginx/.htpasswd;
    proxy_pass http://localhost:8080;
}
```

### TLS/SSL

Use Let's Encrypt for HTTPS:

```bash
# Install certbot
sudo apt install certbot python3-certbot-nginx

# Obtain certificate
sudo certbot --nginx -d indexer.example.com
```

### Security Best Practices

1. **Run as non-root user** (already done via systemd)
2. **Restrict file permissions** (750 for directories, 640 for files)
3. **Enable firewall** (ufw or iptables)
4. **Keep system updated** (regular security patches)
5. **Monitor access logs** (detect suspicious activity)
6. **Use private RPC endpoint** (don't expose to public)
7. **Enable audit logging** (optional, for compliance)

---

## Reference

### Directory Structure

```
/opt/indexer-go/
├── bin/
│   └── indexer-go         # Binary
└── backup/                # Binary backups

/etc/indexer-go/
└── config.yaml            # Main configuration (YAML)

/var/lib/indexer-go/
└── data/                  # PebbleDB data

/var/log/indexer-go/
├── *.log                  # Application logs (if file logging)
└── (systemd journal)      # Default logging
```

### Service Commands

```bash
# Status
sudo systemctl status indexer-go

# Start/Stop/Restart
sudo systemctl start|stop|restart indexer-go

# Enable/Disable autostart
sudo systemctl enable|disable indexer-go

# View logs
sudo journalctl -u indexer-go -f

# Reload daemon
sudo systemctl daemon-reload
```

### Health Check Endpoints

```bash
GET /health              # System health status
GET /version             # Version information
GET /metrics             # Prometheus metrics
GET /subscribers         # EventBus subscriber statistics
```

---

## Support

For issues and questions:
- GitHub Issues: https://github.com/0xmhha/indexer-go/issues
- Documentation: https://github.com/0xmhha/indexer-go/tree/main/docs

---

**Last Updated**: 2025-10-20
**Version**: 1.0.0
