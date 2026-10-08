---
title: "Runbook: Rotating the Database Password"
author: Platform Team
date: 2026-07-03
version: "1.1"
---

# Runbook: Rotating the Database Password

Use this runbook to change the password the order service uses for its
database. It is done every 90 days, and at once if the password may have
leaked.

| | |
| --- | --- |
| **Duration** | About 20 minutes |
| **Downtime** | None, if the steps are followed in order |
| **Who** | An engineer on the platform rota |
| **When** | Any time outside the Friday release window |

## Before you start

- [ ] You have the `db-admin` role in the production project.
- [ ] `kubectl` points at the production cluster: `kubectl config current-context`
- [ ] Nobody else is working on the database. Ask in `#platform`.

> [!CAUTION]
> The old password keeps working until step 4. Do not skip ahead: revoking
> it while pods still use it takes the service down.

## Steps

1. **Generate a new password** and keep it in your shell only:

   ```sh
   NEW_PASSWORD="$(openssl rand -base64 32)"
   ```

2. **Add it to the database** as a second valid password. The role
   `orders_next` exists for this:

   ```sql
   ALTER ROLE orders_next WITH PASSWORD :'new_password';
   GRANT orders TO orders_next;
   ```

   Check that it works before going on:

   ```sh
   PGPASSWORD="$NEW_PASSWORD" psql -h db.internal -U orders_next -d orders -c 'SELECT 1'
   ```

   Expected output:

   ```text
    ?column?
   ----------
           1
   (1 row)
   ```

3. **Give the service the new password** and restart it one pod at a time:

   ```sh
   kubectl -n orders create secret generic db-credentials \
       --from-literal=username=orders_next \
       --from-literal=password="$NEW_PASSWORD" \
       --dry-run=client -o yaml | kubectl apply -f -
   kubectl -n orders rollout restart deployment/order-service
   kubectl -n orders rollout status deployment/order-service --timeout=5m
   ```

   The rollout is finished when every pod is `Running` and `READY 1/1`.

4. **Retire the old password.** Only after the rollout has finished:

   ```sql
   ALTER ROLE orders WITH PASSWORD NULL;
   ```

5. **Swap the role names** so that the next rotation starts from the same
   place: `orders_next` becomes `orders`, and the other way around.

## Check that it worked

- The error rate on the *Order service* dashboard is where it was before.
- New orders arrive: `SELECT max(created_at) FROM orders;` is less than a
  minute old.
- No pod logs `password authentication failed`:

  ```sh
  kubectl -n orders logs deployment/order-service --since=10m | grep -c "authentication failed"
  ```

  The count should be `0`.

## If something goes wrong

| Symptom | Likely cause | What to do |
| --- | --- | --- |
| Pods restart in a loop after step 3 | The secret has a typo, or the wrong role name | Run `kubectl rollout undo deployment/order-service`; the old password still works |
| `psql` in step 2 says `role "orders_next" does not exist` | The roles were not swapped after the last rotation | Use `orders_prev` in place of `orders_next`, and fix the names in step 5 |
| Errors start after step 4 | Something else used the old password | Set it back (it is in the vault under `orders/previous`) and find what it is |

To go back at any point before step 4, undo the rollout. After step 4, set
the old password again from the vault, then undo the rollout.

## Afterwards

1. Store the new password in the vault under `orders/current`, and move the
   previous one to `orders/previous`.
2. Note the rotation in the change log, with the date and your name.
3. If you had to leave the runbook, say where and why in `#platform`, so that
   the runbook can be corrected.
