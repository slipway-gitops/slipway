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


```
cat <<'EOF' > kustomization.yaml
bases:
- https://github.com/slipway-gitops/slipway/config/default/?ref=master
secretGenerator:
- name: slipwaykey
  namespace: slipway-system
  files:
  - id_rsa
images:
- name: controller
  newName: slipway/slipway
  newTag: 0.1.1
generatorOptions:
  disableNameSuffixHash: true
EOF
```

Now in the same folder you should be able to run:
```
kubectl apply -k .
```

If you want to try an example gitrepo you can try out:
```
kubectl apply -k https://github.com/slipway-gitops/slipway-examples/gitrepo
```

This will deploy everything in a namespace called master


