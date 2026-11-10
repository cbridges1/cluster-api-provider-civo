#!/usr/bin/env python3
"""Assembles the clusterctl/Turtles release artifacts into out/:
infrastructure-components.yaml (CRDs, RBAC, the manager), metadata.yaml, and
the ClusterClasses in templates/ (clusterclass-NAME.yaml), which clusterctl
looks for beside the components.

    hack/components.py ghcr.io/cbridges1/cluster-api-provider-civo:v0.1.0
"""
import glob, os, shutil, sys
import yaml

PROVIDER_LABEL = {"cluster.x-k8s.io/provider": "infrastructure-civo"}
CONTRACT_LABEL = {"cluster.x-k8s.io/v1beta2": "v1alpha1"}

image = sys.argv[1] if len(sys.argv) > 1 else "ghcr.io/cbridges1/cluster-api-provider-civo:latest"
root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
docs = []
for f in sorted(glob.glob(os.path.join(root, "config/crd/*.yaml"))):
    for d in yaml.safe_load_all(open(f)):
        if d:
            d["metadata"].setdefault("labels", {}).update(CONTRACT_LABEL)
            docs.append(d)
for d in yaml.safe_load_all(open(os.path.join(root, "config/rbac/role.yaml"))):
    if d:
        d["metadata"]["name"] = "capcivo-manager-role"
        docs.append(d)
for d in yaml.safe_load_all(open(os.path.join(root, "config/manager/manager.yaml"))):
    if d:
        docs.append(d)
for d in docs:
    d["metadata"].setdefault("labels", {}).update(PROVIDER_LABEL)
    if d["kind"] == "Deployment":
        for c in d["spec"]["template"]["spec"]["containers"]:
            c["image"] = image

out = os.path.join(root, "out")
os.makedirs(out, exist_ok=True)
with open(os.path.join(out, "infrastructure-components.yaml"), "w") as fh:
    yaml.safe_dump_all(docs, fh, sort_keys=False)
with open(os.path.join(out, "metadata.yaml"), "w") as fh:
    yaml.safe_dump({
        "apiVersion": "clusterctl.cluster.x-k8s.io/v1alpha3",
        "kind": "Metadata",
        # One entry per minor release: clusterctl refuses a version whose
        # series isn't listed.
        "releaseSeries": [{"major": 0, "minor": 1, "contract": "v1beta2"}, {"major": 0, "minor": 2, "contract": "v1beta2"}, {"major": 0, "minor": 3, "contract": "v1beta2"}],
    }, fh, sort_keys=False)
classes = sorted(glob.glob(os.path.join(root, "templates/clusterclass-*.yaml")))
for f in classes:
    shutil.copy(f, out)
print(f"out/infrastructure-components.yaml ({len(docs)} objects, image {image}), out/metadata.yaml and "
      + ", ".join("out/" + os.path.basename(f) for f in classes))
