module.exports = {
  extends: ["@commitlint/config-conventional"],
  ignores: [(message) => message.startsWith("Merge ")],
  parserPreset: {
    parserOpts: {
      headerPattern: /^([a-zA-Z]+)(\([^)]+\))?:\s*(.*)$/,
      headerCorrespondence: ["type", "scope", "subject"],
    },
  },
  rules: {
    "subject-empty": [0],
  },
};
