import { describe, expect, it } from "vitest";
import {
  checkField,
  detectProblem,
  javaOf,
  listCommand,
  mcDate,
  minecraftKind,
  motdLines,
  parseList,
  parseProperties,
  pickJavaImage,
  PROPERTY_GROUPS,
  setProperties,
  startsRun,
  worldFolders,
} from "./minecraft";

describe("minecraftKind", () => {
  it("reads the egg's features", () => {
    expect(minecraftKind(["eula", "java_version", "minecraft"])).toBe("server");
    expect(minecraftKind(["eula"])).toBe("server"); // a Pterodactyl community egg
    expect(minecraftKind(["java_version", "minecraft_proxy"])).toBe("proxy");
    expect(minecraftKind(["pid_limit"])).toBeNull();
    expect(minecraftKind(undefined)).toBeNull();
  });
});

describe("detectProblem", () => {
  it("finds an unaccepted EULA", () => {
    expect(detectProblem("[gamepanel] accept the Minecraft EULA (https://aka.ms/MinecraftEULA) first: set ...")).toEqual({ kind: "eula" });
    expect(detectProblem("\x1b[33m[10:00:00 WARN]: You need to agree to the EULA in order to run the server.\x1b[0m")).toEqual({
      kind: "eula",
    });
  });
  it("finds a Java that is too old", () => {
    expect(detectProblem("[gamepanel] this server needs Java 21 or newer but the image has Java 17. Pick ...")).toEqual({
      kind: "java",
      need: 21,
      have: 17,
    });
    expect(
      detectProblem(
        "Error: LinkageError occurred while loading main class io.papermc.paperclip.Main\tjava.lang.UnsupportedClassVersionError: io/papermc/paperclip/Main has been compiled by a more recent version of the Java Runtime (class file version 65.0), this version of the Java Runtime only recognizes class file versions up to 61.0",
      ),
    ).toEqual({ kind: "java", need: 21, have: 17 });
    expect(detectProblem("java.lang.UnsupportedClassVersionError: Main (class file version 69.0)")).toEqual({ kind: "java", need: 25 });
  });
  it("knows the lines of a newer run", () => {
    expect(startsRun("[gamepanel] Java 25 | limit 2048 MB | heap 1444 MB | GC g1")).toBe(true);
    expect(startsRun('\x1b[0m[14:00:12 INFO]: Done (14.539s)! For help, type "help"')).toBe(true);
    expect(startsRun("[14:00:01 INFO]: Starting minecraft server version 26.3")).toBe(true);
    expect(startsRun("<Steve> Done (1s)? no")).toBe(false);
  });
  it("ignores ordinary lines", () => {
    expect(detectProblem('[10:00:00 INFO]: Done (3.2s)! For help, type "help"')).toBeNull();
    expect(detectProblem("Player said: class file version 65.0 lol")).toBeNull();
  });
});

describe("java images", () => {
  const images = [
    { label: "Java 25", image: "ghcr.io/zsombiihun/gamepanel-java:25" },
    { label: "Java 21", image: "ghcr.io/zsombiihun/gamepanel-java:21" },
    { label: "Java 17", image: "ghcr.io/zsombiihun/gamepanel-java:17" },
    { label: "Java 8", image: "ghcr.io/zsombiihun/gamepanel-java:8" },
  ];
  it("reads the version from a label or a tag", () => {
    expect(javaOf({ label: "ghcr.io/pterodactyl/yolks:java_21", image: "ghcr.io/pterodactyl/yolks:java_21" })).toBe(21);
    expect(javaOf({ label: "Custom", image: "eclipse-temurin:17-jre" })).toBe(17);
    expect(javaOf({ label: "Custom", image: "debian:bookworm" })).toBeNull();
  });
  it("picks the oldest image that is new enough", () => {
    expect(pickJavaImage(images, 21)?.label).toBe("Java 21");
    expect(pickJavaImage(images, 18)?.label).toBe("Java 21");
    expect(pickJavaImage(images, 26)).toBeNull();
  });
});

describe("server.properties", () => {
  const file = [
    "#Minecraft server properties",
    "#Thu Nov 20 10:00:00 UTC 2026",
    "motd=A Minecraft Server",
    "max-players=20",
    "level-seed=",
    "server-port=25565",
    "custom\\:key = with spaces",
    "long=one \\",
    "    two",
    "",
  ].join("\n");

  it("parses keys, escapes and continued lines", () => {
    const p = parseProperties(file);
    expect(p.get("motd")).toBe("A Minecraft Server");
    expect(p.get("level-seed")).toBe("");
    expect(p.get("custom:key")).toBe("with spaces");
    expect(p.get("long")).toBe("one two");
    expect(p.has("#Minecraft server properties")).toBe(false);
  });

  it("changes only the given keys and appends new ones", () => {
    const out = setProperties(file, { "max-players": "50", motd: "§aHi: ünïcode", pvp: "false" });
    const lines = out.split("\n");
    expect(lines[0]).toBe("#Minecraft server properties");
    expect(lines[2]).toBe("motd=\\u00a7aHi\\: \\u00fcn\\u00efcode");
    expect(lines[3]).toBe("max-players=50");
    expect(lines).toContain("custom\\:key = with spaces");
    expect(lines).toContain("pvp=false");
    expect(out.endsWith("\n")).toBe(true);
    // What we write reads back the same.
    const p = parseProperties(out);
    expect(p.get("motd")).toBe("§aHi: ünïcode");
    expect(p.get("long")).toBe("one two");
  });

  it("works on an empty file", () => {
    expect(setProperties("", { a: " x" })).toBe("a=\\ x\n");
    expect(parseProperties("a=\\ x\n").get("a")).toBe(" x");
  });
});

describe("checkField", () => {
  it("allows the MOTD two lines and other text one", () => {
    const motd = PROPERTY_GROUPS.flatMap((g) => g.fields).find((f) => f.key === "motd");
    expect(motd && checkField(motd, "one\ntwo")).toBeNull();
    expect(motd && checkField(motd, "one\ntwo\nthree")).toBe("At most 2 lines.");
    expect(checkField({ key: "level-name", label: "", type: "text", def: "" }, "a\nb")).toBe("Keep it on one line.");
  });
});

describe("motdLines", () => {
  it("splits colour and style codes", () => {
    const [l1, l2] = motdLines("§aGreen §lbold§r plain\n§cred &cnot");
    expect(l1).toEqual([{ text: "Green ", color: "#55FF55" }, { text: "bold", color: "#55FF55", bold: true }, { text: " plain" }]);
    expect(l2).toEqual([{ text: "red &cnot", color: "#FF5555" }]);
  });
  it("leaves unknown codes as text", () => {
    expect(motdLines("100§ off §z")).toEqual([[{ text: "100§ off §z" }]]);
  });
});

describe("player lists", () => {
  it("reads missing or broken files as empty", () => {
    expect(parseList(undefined)).toEqual([]);
    expect(parseList("{oops")).toEqual([]);
    expect(parseList('{"a":1}')).toEqual([]);
    expect(parseList<{ name: string }>('[{"name":"Alex"}]')[0]?.name).toBe("Alex");
  });
  it("builds the console commands", () => {
    expect(listCommand("whitelist", "add", "Alex")).toBe("whitelist add Alex");
    expect(listCommand("ops", "remove", "Alex")).toBe("deop Alex");
    expect(listCommand("bans", "add", "Alex", " griefing ")).toBe("ban Alex griefing");
    expect(listCommand("ipbans", "remove", "1.2.3.4")).toBe("pardon-ip 1.2.3.4");
  });
  it("writes dates as Minecraft does", () => {
    expect(mcDate(new Date(Date.UTC(2026, 10, 20, 9, 5, 3)))).toBe("2026-11-20 09:05:03 +0000");
  });
  it("names a world's folders", () => {
    expect(worldFolders("")).toEqual(["world", "world_nether", "world_the_end"]);
  });
});
