# Syncing products, BOMs and partners between Odoo databases

`odoo-sync` copies categories, attributes, product templates, variants, bills of materials and
partners from a source Odoo to a target Odoo through JSON files. It has three command groups,
`products`, `boms` and `partners`, each with its own file and with `export`, `check` and `import`
commands. The source is the master; the target is a replica. It reads the source and writes the
target only, and never deletes: records missing from the file are not removed, and records archived
in the source are archived in the target.

```sh
go build -o target/odoo-sync ./cmd/odoo-sync
target/odoo-sync products export -source configs/source.yaml -out data/products.json
target/odoo-sync products check  -in data/products.json
target/odoo-sync products check  -in data/products.json -target configs/target.yaml
target/odoo-sync products import -target configs/target.yaml -in data/products.json -dry-run
target/odoo-sync products import -target configs/target.yaml -in data/products.json -map-out data/map.csv
target/odoo-sync boms export -source configs/source.yaml -out data/boms.json
target/odoo-sync boms check  -in data/boms.json -target configs/target.yaml
target/odoo-sync boms import -target configs/target.yaml -in data/boms.json -dry-run
target/odoo-sync partners export -source configs/source.yaml -out data/partners.json
target/odoo-sync partners export -source configs/source.yaml -out data/partners.json --infer-country-from-state
target/odoo-sync partners check  -in data/partners.json -target configs/target.yaml
target/odoo-sync partners import -target configs/target.yaml -in data/partners.json -dry-run
```

Connection files are read by `odoo.LoadClient` (`odoo.base_url`, `odoo.api_key`, optional
`odoo.db`). Both databases need the sales and inventory modules, because product reads request
`is_storable`. BOMs also need the Manufacturing module (`mrp`); on a database without it, use only
the `products` and `partners` groups. Partners need only `base`.

`check -in` validates the file and the default import policy: it reports uncoded product variants,
BOM products or components without a code, and top-level partners without a match key. Use
`-empty-code skip`, `-empty-key skip` or `-sale-ok only` to check with the matching import policy.
Add `-target` for a read-only import preflight against the target database, including matching,
missing dependencies and unit conversions. `-no-create-deps` requires `-target`. A passing preflight
does not guarantee that later writes will succeed; Odoo can reject a write or change between check
and import.

Product and BOM exports include each unit of measure they use, plus its reference units, in the
`units` section. Odoo 19 represents conversions with `relative_uom_id` and `relative_factor`; the
file stores the reference unit by name. Import creates missing units before products or BOMs. An
existing unit with the same name must have the same reference and factor, or the import stops before
writing. Catalog files now use format 2 with a single `section` value (`products` or `boms` from
the CLI; `all` is accepted for combined files). Format 1 files with an `include` array are rejected
and must be exported again. Partner files keep their separate format 1 schema.

## Matching

Variants are matched between databases by `default_code` (the "Internal Reference") only. Odoo ids
differ per database, so the file keeps each source id as `source_id` for reference and the importer
ignores it. A template is matched through its variants. Categories match by full path, attributes by
name, attribute values by attribute and name, and units of measure by name. Missing units are
created from the file's `units` definitions.

The export leaves out archived variants that have no `default_code` and prints how many: they cannot
be matched, and they usually repeat the value combination of the variant that replaced them.
Active variants without `default_code` stay in the file; export warns how many were included, since
the default `-empty-code reject` import policy will refuse them.

## Import options

- `-dry-run` prints what would be created, updated or archived and writes nothing.
- `-empty-code reject` (default) refuses the whole import when a variant has no `default_code` and
  lists them; `skip` skips the whole template that holds such a variant and reports it. For BOMs it
  covers a product or a component without a code: `skip` skips that BOM and archives no BOM of its
  product, since the target copy of a skipped BOM cannot be judged.
- `-sale-ok all` (default) imports every template; `only` imports templates with `sale_ok` set.
- `-no-create-deps` fails instead of creating missing units, categories, attributes and values.
- `-map-out file.csv` writes a CSV with the columns default_code, source_variant_id,
  target_variant_id and target_template_id. Use it to translate ids that another system stored from
  the source. Only `products import` has it; `boms import` rejects it.

`-sale-ok` belongs to `products import`; `boms import` still accepts it but ignores it. The partners
options are described under [Partners](#partners).

## What is checked before anything is written

The importer plans the whole import first and stops, writing nothing, on any of these: a variant
without `default_code` (unless skipped), a `default_code` twice in the file, one code matching
several target variants, only some variants of a template existing in the target, variants of one
template belonging to different target templates, a missing unit of measure, and (with
`-no-create-deps`) a missing category, attribute or value.

## Existing records

A matched variant is overwritten from the file: name, category, list price, `sale_ok`, `type`,
`is_storable`, description, unit, barcode (when the file has one), weight and volume. The extra
price of an attribute value is set on the template attribute value, where Odoo keeps it. Fields that
already match cause no request.

## Failures

Writes are not retried, and Odoo has no transaction across requests. A failure in one template is
reported and the run continues; the exit status is non-zero.

If a template was created but a later step failed, the message names the template id. The target
now holds a template whose variants may carry some `default_code` values and not others, and the
next run refuses to merge such a template. Clear the `default_code` of its variants, or finish it by
hand in the target, before rerunning. Archiving it does not help, because archived variants are
still matched by code. Nothing is written onto a template until every variant the file lists has
been generated by Odoo, so the common case (Odoo did not generate a combination) leaves no codes
behind.

## Attributes

A new template can only use target attributes whose `create_variant` is `always`; otherwise Odoo
would not generate every combination, and the import stops with a problem naming the attribute. An
archived attribute or attribute value in the target also stops the import: restore it in Odoo first.
When Odoo generates a combination that the file does not list (for example one excluded or archived
in the source), the variant is archived and reported as a warning.

## Bills of materials

A BOM names its product by `default_code`: a BOM for one variant carries `product.default_code`, a
BOM for a whole template carries `template.default_codes`, the sorted codes of the template's
active, coded variants. Components are referenced by `default_code` too. The BOM `code` is not a key
(it is neither unique nor always set); it is copied as an ordinary field.

A BOM's identity is its content fingerprint: type, produced quantity, unit and the sorted
(component, quantity, unit) lines. The reference, line order, source id and active state are not
part of it. For each product that the file has a BOM for, the importer compares the active BOMs in
the file with the active BOMs in the target:

- same fingerprint: kept, and only its reference is synced;
- in the file but not in the target: created;
- in the target but not in the file: **archived**, never deleted, because manufacturing orders may
  still point at it;
- archived in the file: never created. A product whose file BOMs are all archived loses every
  active BOM in the target; an archived BOM only needs its product to exist.

So when the source edits the components of a BOM, the target ends with the old BOM archived and a
new one created, not an edited one. Two active BOMs with the same content for one product count as
one: the importer warns and keeps one, which also archives a duplicate that the target holds.
Products the file has no BOM for are not touched.

Import products first, then BOMs. Every product and component must already exist in the target; a
missing one stops the import and is listed, and nothing is created for it. Creates run first, then
reference updates, then archives, so a product never loses its old BOM before the new one exists; if
a create fails, that product keeps its old BOM and the failure is reported.

A `default_code` that matches several variants in the target stops the import.

The import also stops on a missing unit of measure (BOM or line) and, unless `-empty-code skip`, on
a product or component without a `default_code`. The export refuses a source BOM that has
operations, byproducts or lines restricted by attribute value, because the file cannot carry them.

## Partners

`odoo-sync partners export` writes every partner, archived ones included, to a JSON file;
`partners import` copies them into the target. The export stops with an error, naming the partner,
when a partner has no name (typical for an unnamed invoice or delivery address); give it a name in
the source first. The commands are `export`, `check` and `import`, and `import` takes `-dry-run`,
`-empty-key reject|skip` and `-no-create-deps`. Partners need only the `base` module.

With `partners export --infer-country-from-state`, a partner whose country is empty but whose state
has a country in the source Odoo gets that country and state in the export file. The exporter prints
a warning naming the inferred country. Without the flag, it warns and omits the state; the partner
is still exported. If a nonempty partner country conflicts with the state's country, the state is
omitted under either policy. The source database is never changed.

Top-level partners are matched in the target in priority passes over the whole file. All partners
with a `ref` are matched first; the remaining ones are then matched by `vat` among the target
partners not already claimed, and then by `email` (case-insensitive), skipping a key that is empty
in the file. A key that matches exactly one unclaimed target partner wins, and a key that matches
none falls through to the next. A key that matches several unclaimed target partners stops the
import and lists their ids, as does a pair of file partners that match the same target partner
through the same key. A file partner whose `vat` or `email` only matches a target partner that an
earlier pass already claimed is created as a new partner, because it is a distinct partner in the
source. A top-level partner with none of the three keys stops the import by default;
`-empty-key skip` skips it and its children. A child is matched below its already-matched parent by
`type` and name, so a child of a partner created in the same run is created too. Target partners
that have a parent never match a top-level one.

A matched partner is overwritten from the file: name, `ref`, contact and address fields, `vat`,
company registry, job position, language, notes, barcode, type, `is_company`, country, state and
tags. Fields that already match cause no request. The active state is copied: archived partners are
archived in the target and restored when the file says so. Archives run before restores, and after a
company is archived the importer also restores the descendants that the file marks active, because
Odoo archives a company's children together with it. Nothing is deleted.

The import stops, before any write, when a partner's `lang` is not installed in the target. Country
is matched by name and state by country and name; a missing one stops the import and is listed, and
neither is created. Tags are matched by name and missing ones are created (with `-no-create-deps`
they stop the import); parent tags are not recreated. The salesperson, company, industry and the
create and modify dates are not exported.

The importer plans the whole import before writing and stops on any problem: a duplicate key in the
file, a missing parent, a missing country, state or (with `-no-create-deps`) tag, an ambiguous
match, or a partner without a key. `-dry-run` prints the plan (`create`, `update`, `skip`, `archive`
and `restore` lines and a `summary:` line) and writes nothing. A real run ends with
`created N, updated N, unchanged N, skipped N, archived N, restored N, failed N`. The real run may
print a larger `restored` count than the dry-run's `to restore`, because it also restores the
children of a company it archives (Odoo archives them together with the company).

## Partner failures

Writes are not retried and Odoo has no transaction across requests. A failed partner is reported as
`failed:` and the run continues, and the exit status is non-zero. The children of a partner whose
create failed, or that was not written, are reported as failed and skipped. A failed update of a
matched parent does not skip its children. After a failed create, search the target by `ref`, `vat`
or `email` before running again. Two imports into one target at the same time may create duplicates.

## Verification status

Everything above, partners included, is covered by unit tests against a fake Odoo server and by
read-only runs (`export`, `check` and `import -dry-run`) against a real source. No import has been
run against a real Odoo yet, so the write behavior below is **not verified**. Treat it as the
intended design, and run the write steps in [manual-testing.md](manual-testing.md#odoo-sync-tests)
on a disposable test database before relying on it. This section is to be updated, or removed, once
they have been run.

- How Odoo generates variants after attribute lines are added through JSON-2, and the write-back of
  the extra price of an attribute value.
- That `SetProductActive` archives a single variant, and that archiving every variant of a template
  matches archiving the template.
- The line commands that `CreateBOM` sends to create a BOM with its lines in one request.
- `UpdateBOMCode`, the in-place update of a BOM reference.
- Partner writes: the planning, matching and apply logic and the first import path (create with a
  parent, tag creation, the field overwrite, archive and restore) are covered by tests against a
  fake server. The idempotence of a second import, including how Odoo normalizes written values, is
  **not verified**; check it on a test database (step 9 in
  [manual-testing.md](manual-testing.md#odoo-sync-tests)).
- Idempotence: a second import of the same file reporting no change, for products and for BOMs.
- Whether Odoo rejects a BOM whose variant belongs to another template; the importer does not check.
- The HTTP status Odoo returns for BOM calls without the `mrp` module ([modules.md](modules.md)
  says 500; it may be 404).

## Not covered

Operations (work centers), byproducts, attribute-restricted BOM lines, stock quantities, price
lists, taxes, images, vendor prices, and rewriting ids that other systems stored from the source.
For partners: bank accounts, avatars, the salesperson, and the partner tag hierarchy.
