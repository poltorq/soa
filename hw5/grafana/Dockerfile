FROM grafana/grafana:12.4.16
USER root
RUN mkdir -p /opt/grafana-plugins && \
    grafana cli --pluginsDir /opt/grafana-plugins plugins install grafana-clickhouse-datasource 4.24.0 && \
    chown -R 472:0 /opt/grafana-plugins
ENV GF_PATHS_PLUGINS=/opt/grafana-plugins
USER 472
