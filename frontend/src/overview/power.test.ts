import { describe, expect, it } from "vitest";
import { gridOnly, pvGrid, r1, r3, r4 } from "./fixtures";
import {
  directed,
  liveKey,
  meterLabel,
  overviewKeys,
  powerState,
  type Lookup,
} from "./power";

const lookup =
  (m: Record<string, number>): Lookup =>
  (k) =>
    m[k];

describe("liveKey", () => {
  it("appends .power except for site/soc", () => {
    expect(liveKey("site/pv")).toBe("site/pv.power");
    expect(liveKey("site/loadpoints/wallbox")).toBe(
      "site/loadpoints/wallbox.power",
    );
    expect(liveKey("site/soc")).toBe("site/soc");
  });
});

describe("overviewKeys", () => {
  it("includes site and meter power keys, unique and sorted", () => {
    const keys = overviewKeys(r4);
    expect(keys).toContain("site/soc");
    expect(keys).toContain("site/loadpoints/wallbox.power");
    for (const id of ["pv1", "pv2", "bat", "backup", "goe", "warp", "shelly"]) {
      expect(keys).toContain(`${id}/power`);
    }
    expect(new Set(keys).size).toBe(keys.length);
    expect(keys).toEqual([...keys].sort());
  });
  it("skips meters without a power series", () => {
    const s = { ...gridOnly, meters: [{ ...gridOnly.meters[0]!, series: [] }] };
    expect(overviewKeys(s).some((k) => k.startsWith("grid/"))).toBe(false);
  });
});

describe("powerState nodes", () => {
  const kinds = (s: Parameters<typeof powerState>[0]) =>
    powerState(s, lookup({})).nodes.map((n) => n.kind);
  it.each([
    [r1, ["pv", "grid", "battery", "house"]],
    [r3, ["pv", "grid", "battery", "house"]],
    [r4, ["pv", "grid", "battery", "house", "wallbox"]],
    [pvGrid, ["pv", "grid", "house"]],
    [gridOnly, ["grid", "house"]],
  ])("order and presence %#", (s, want) => {
    expect(kinds(s)).toEqual(want);
  });
  it("counts meters per node", () => {
    const n = powerState(r4, lookup({})).nodes;
    expect(n.find((x) => x.kind === "pv")?.count).toBe(2);
    expect(n.find((x) => x.kind === "wallbox")?.meters).toEqual([
      "goe",
      "warp",
    ]);
    expect(n.find((x) => x.kind === "grid")?.meters).toEqual(["shelly"]);
    expect(n.find((x) => x.kind === "house")?.count).toBe(0);
    expect(n.find((x) => x.kind === "house")?.meters).toEqual(["backup"]);
  });
  it("counts PV strings below a PV total as part of it", () => {
    const s = {
      ...r1,
      meters: [
        {
          id: "pv",
          role: "pv" as const,
          label: "Roof",
          device: "wr1",
          parent: "site",
          series: [],
        },
        ...r1.meters.map((m) => (m.role === "pv" ? { ...m, parent: "pv" } : m)),
      ],
    };
    expect(
      powerState(s, lookup({})).nodes.find((x) => x.kind === "pv")?.meters,
    ).toEqual(["pv"]);
  });
  it("one node per loadpoint category", () => {
    const s = {
      ...r4,
      capabilities: {
        ...r4.capabilities,
        loadpoints: { wallbox: 2, heatpump: 1, other: 0 },
      },
    };
    const k = kinds(s);
    expect(k.filter((x) => x === "wallbox")).toHaveLength(1);
    expect(k).toContain("heatpump");
    expect(k).not.toContain("other");
  });
});

describe("powerState values", () => {
  const live = lookup({
    "site/pv.power": 3000,
    "site/grid_in.power": 500,
    "site/grid_out.power": 200,
    "site/battery_in.power": 100,
    "site/battery_out.power": 900,
    "site/house_rest.power": 700,
    "site/loadpoints/wallbox.power": 1400,
    "backup/power": 50,
    "site/soc": 63,
  });
  it("signs: grid import - export, battery discharge - charge", () => {
    const n = powerState(r4, live).nodes;
    const p = (k: string) => n.find((x) => x.kind === k)?.power;
    expect(p("pv")).toBe(3000);
    expect(p("grid")).toBe(300);
    expect(p("battery")).toBe(800);
    expect(p("house")).toBe(700);
    expect(p("wallbox")).toBe(1400);
    expect(powerState(r4, live).soc).toBe(63);
  });
  it("negative when exporting or charging", () => {
    const l = lookup({
      "site/grid_in.power": 0,
      "site/grid_out.power": 1200,
      "site/battery_in.power": 400,
      "site/battery_out.power": 0,
    });
    const n = powerState(r1, l).nodes;
    expect(n.find((x) => x.kind === "grid")?.power).toBe(-1200);
    expect(n.find((x) => x.kind === "battery")?.power).toBe(-400);
  });
  it("missing values stay undefined, never 0", () => {
    const st = powerState(r4, lookup({ "site/grid_in.power": 5 }));
    for (const n of st.nodes) expect(n.power).toBeUndefined();
    expect(st.soc).toBeUndefined();
  });
  it("house is the house rest, the backup output inside it", () => {
    const n = powerState(r4, live).nodes;
    expect(n.find((x) => x.kind === "house")?.power).toBe(700);
  });
  it("socApprox only when a battery lacks capacity_wh", () => {
    expect(powerState(r1, live).socApprox).toBe(false);
    const s = {
      ...r1,
      meters: r1.meters.map((m) =>
        m.role === "battery" ? { ...m, capacity_wh: undefined } : m,
      ),
    };
    expect(powerState(s, live).socApprox).toBe(true);
    expect(powerState(pvGrid, live).socApprox).toBe(false);
  });
});

describe("meterLabel", () => {
  const text = { backup: "Notstrom", ofWhich: (n: string) => `davon ${n}` };
  it("uses the label, the ID when unlabelled", () => {
    const s = {
      ...r4,
      meters: [...r4.meters, { ...r4.meters[0]!, id: "x", label: "Süd" }],
    };
    expect(meterLabel(s, "x", text)).toBe("Süd");
    expect(meterLabel(r4, "goe", text)).toBe("goe");
    expect(meterLabel(r4, "unknown", text)).toBe("unknown");
  });
  it("reads a backup output as part of House", () => {
    expect(meterLabel(r4, "backup", text)).toBe("davon Notstrom");
    const s = {
      ...r4,
      meters: r4.meters.map((m) =>
        m.id === "backup" ? { ...m, label: "Keller" } : m,
      ),
    };
    expect(meterLabel(s, "backup", text)).toBe("davon Keller");
  });
});

describe('directed', () => {
  it('spells out battery and grid directions as magnitudes', () => {
    expect(directed('battery', -600)).toEqual({ power: 600, dir: 'charging' })
    expect(directed('battery', 300)).toEqual({ power: 300, dir: 'discharging' })
    expect(directed('grid', 400)).toEqual({ power: 400, dir: 'import' })
    expect(directed('grid', -500)).toEqual({ power: 500, dir: 'export' })
    expect(directed('battery', 0)).toEqual({ power: 0, dir: null })
    expect(directed('pv', 2000)).toEqual({ power: 2000, dir: null })
    expect(directed('grid', undefined)).toEqual({ power: undefined, dir: null })
  })
})
