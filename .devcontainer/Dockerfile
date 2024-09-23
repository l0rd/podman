FROM quay.io/podman/stable

COPY /rpm/podman.spec /rpm/podman.spec

RUN dnf update -y && dnf -y install 'dnf-command(builddep)'

RUN dnf -y \
    builddep /rpm/podman.spec
