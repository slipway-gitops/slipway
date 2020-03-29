# Deploying Slipway

***REMINDER THIS IS NOT READY FOR PRODUCTION***

To deploy Slipway you will need to have a
[ssh key on your Github account](https://help.github.com/en/github/authenticating-to-github/connecting-to-github-with-ssh),
even if you are using public repositories.

First we will make a temp directory to work from.

```bash
tmp_dir=$(mktemp -d)
cd $tmp_dir
```

Generate a ssh key
```
ssh-keygen -f id_rsa
```

Move your public ssh key to your Github account.

Now we will setup kustomize to generate a Secret from your key, and load it into the controller

First create the namespace.
```
kubectl create ns slipway-system
```

```bash
cat <<'EOF' > kustomization.yaml
secretGenerator:
- name: slipwaykey
  namespace: slipway-system
  files:
  - id_rsa
generatorOptions:
  disableNameSuffixHash: true
EOF
```

Now in the same folder you will run:
```
kubectl apply -k .
kubectl apply -f https://github.com/slipway-gitops/slipway/releases/latest/download/fulldeploy.yaml
```

If you want to try an example gitrepo you can try out the
[examples repo](https://github.com/slipway-gitops/slipway-example-gitrepo)
```

