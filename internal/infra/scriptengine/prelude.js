// The `pm` API a script is written against, in JS, over a handful of Go functions. It is here and not
// in Go because it is a language rather than a translation: the assertion chain, the header list and
// the way a logged value prints are all things a person reads while writing a script.
//
// The bridge is the only way out of the sandbox — a script cannot reach a file, a socket or a timer —
// and it is deleted before the script itself runs, so all a script sees is `pm` and `console`.
//
// Kept to ES5.1-and-a-half on purpose: no optional chaining, no `??`, no `flat`. What a sandbox
// supports should be obvious from reading it, not from knowing which goja release this is.
(function (bridge) {
  'use strict';

  // A value as it reads in a log line: strings are themselves, everything else is JSON when it can be,
  // because an object printed as `[object Object]` is a log line that hides the thing it logged.
  const print = (value) => {
    if (typeof value === 'string') return value;
    if (typeof value === 'undefined') return 'undefined';
    if (value === null) return 'null';
    try {
      const json = JSON.stringify(value);
      if (typeof json === 'string') return json;
    } catch (error) {
      // Куда ссылается круговая ссылка, JSON не пишет: печатаем как есть.
    }
    return String(value);
  };

  const text = (value) => (value === undefined || value === null ? '' : String(value));

  const typeOf = (value) => {
    if (value === null) return 'null';
    if (Array.isArray(value)) return 'array';
    return typeof value;
  };

  const TYPE_RU = {
    string: 'строка', number: 'число', boolean: 'логическое значение', object: 'объект',
    array: 'массив', null: 'null', undefined: 'undefined', function: 'функция',
  };

  // Comparison by value and not by reference: a test on a parsed response compares what it holds, and
  // two objects with the same keys in another order are the same object to whoever wrote the script.
  const equal = (a, b) => {
    if (a === b) return true;
    if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false;
    const aArray = Array.isArray(a);
    if (aArray !== Array.isArray(b)) return false;
    if (aArray) {
      if (a.length !== b.length) return false;
      for (let i = 0; i < a.length; i++) if (!equal(a[i], b[i])) return false;
      return true;
    }
    const aKeys = Object.keys(a);
    const bKeys = Object.keys(b);
    if (aKeys.length !== bKeys.length) return false;
    for (let i = 0; i < aKeys.length; i++) {
      const key = aKeys[i];
      if (!Object.prototype.hasOwnProperty.call(b, key)) return false;
      if (!equal(a[key], b[key])) return false;
    }
    return true;
  };

  const contains = (haystack, needle) => {
    if (typeof haystack === 'string') return haystack.indexOf(text(needle)) >= 0;
    if (Array.isArray(haystack)) {
      for (let i = 0; i < haystack.length; i++) if (equal(haystack[i], needle)) return true;
      return false;
    }
    if (haystack !== null && typeof haystack === 'object') {
      if (needle === null || typeof needle !== 'object') return false;
      const keys = Object.keys(needle);
      for (let i = 0; i < keys.length; i++) {
        const key = keys[i];
        if (!Object.prototype.hasOwnProperty.call(haystack, key)) return false;
        if (!equal(haystack[key], needle[key])) return false;
      }
      return true;
    }
    return false;
  };

  // Headers as Postman names them — `key` and `value` — and with lookups that ignore case, because
  // HTTP does. A pre-request script adds, replaces and removes here; what it leaves is what is sent.
  const headerList = (items, mutable) => {
    const keyOf = (header) => text(header && header.key !== undefined ? header.key : header && header.name);
    const at = (name) => {
      for (let i = 0; i < items.length; i++) if (keyOf(items[i]).toLowerCase() === text(name).toLowerCase()) return i;
      return -1;
    };
    const list = {
      __items: items,
      get: (name) => {
        const found = at(name);
        return found < 0 ? null : text(items[found].value);
      },
      has: (name) => at(name) >= 0,
      all: () => items.map((item) => ({ key: keyOf(item), value: text(item.value) })),
    };
    if (mutable) {
      list.add = (header) => { items.push({ key: keyOf(header), value: text(header.value) }); };
      list.upsert = (header) => {
        const pair = { key: keyOf(header), value: text(header.value) };
        const found = at(keyOf(header));
        if (found < 0) items.push(pair); else items[found] = pair;
      };
      list.remove = (name) => {
        for (let i = items.length - 1; i >= 0; i--) if (keyOf(items[i]).toLowerCase() === text(name).toLowerCase()) items.splice(i, 1);
      };
    }
    return list;
  };

  // `expect(x).to.be.a('string')` reads well and asserts once, so the words between the two are the
  // same assertion again. `not` is the one that is not decoration.
  class Assertion {
    constructor(value, negated) {
      this.__value = value;
      this.__negated = negated;
    }

    // The two wordings of one failure: what should have been and what was, and the same the other way
    // round, because `не ожидалось 200, получено 200` is a sentence nobody should have to parse.
    __fail(positive, negative) {
      throw new Error(this.__negated ? negative : positive);
    }

    __ok(passed) {
      return passed !== this.__negated;
    }

    equal(expected) {
      if (!this.__ok(this.__value === expected)) {
        this.__fail('ожидалось ' + print(expected) + ', получено ' + print(this.__value),
          'значение равно ' + print(expected) + ', а не должно');
      }
      return this;
    }

    eql(expected) {
      if (!this.__ok(equal(this.__value, expected))) {
        this.__fail('ожидалось ' + print(expected) + ', получено ' + print(this.__value),
          'значение равно ' + print(expected) + ', а не должно');
      }
      return this;
    }

    a(expected) {
      // Имя вида принимается английское — так пишут в Postman, — а в сообщении оно русское: это текст
      // для того, кто читает отчёт, а не для того, кто пишет скрипт.
      const wanted = TYPE_RU[text(expected)] || text(expected);
      const got = TYPE_RU[typeOf(this.__value)] || typeOf(this.__value);
      if (!this.__ok(typeOf(this.__value) === text(expected))) {
        this.__fail('ожидалось ' + wanted + ', а значение — ' + got, 'значение — ' + got + ', а не должно быть');
      }
      return this;
    }

    include(expected) {
      if (!this.__ok(contains(this.__value, expected))) {
        this.__fail('в ' + print(this.__value) + ' нет ' + print(expected),
          'в ' + print(this.__value) + ' есть ' + print(expected) + ', а не должно');
      }
      return this;
    }

    property(name, ...expected) {
      const holds = this.__value !== null && typeof this.__value === 'object' &&
        Object.prototype.hasOwnProperty.call(this.__value, name);
      if (!this.__ok(holds)) {
        this.__fail('у значения нет свойства ' + print(name),
          'у значения есть свойство ' + print(name) + ', а не должно');
      }
      if (expected.length > 0 && !this.__ok(holds && equal(this.__value[name], expected[0]))) {
        this.__fail('свойство ' + print(name) + ' равно ' + print(this.__value[name]) + ', ожидалось ' + print(expected[0]),
          'свойство ' + print(name) + ' равно ' + print(expected[0]) + ', а не должно');
      }
      return this;
    }

    length(expected) {
      const size = this.__value === null || this.__value === undefined ? -1 : this.__value.length;
      if (!this.__ok(size === expected)) {
        this.__fail('длина ' + print(size) + ', ожидалась ' + print(expected),
          'длина ' + print(size) + ', а не должна быть ' + print(expected));
      }
      return this;
    }

    match(expected) {
      const pattern = expected instanceof RegExp ? expected : new RegExp(text(expected));
      if (!this.__ok(pattern.test(text(this.__value)))) {
        this.__fail(print(this.__value) + ' не соответствует ' + pattern,
          print(this.__value) + ' соответствует ' + pattern + ', а не должно');
      }
      return this;
    }

    above(expected) {
      if (!this.__ok(this.__value > expected)) {
        this.__fail(print(this.__value) + ' не больше ' + print(expected),
          print(this.__value) + ' больше ' + print(expected) + ', а не должно');
      }
      return this;
    }

    below(expected) {
      if (!this.__ok(this.__value < expected)) {
        this.__fail(print(this.__value) + ' не меньше ' + print(expected),
          print(this.__value) + ' меньше ' + print(expected) + ', а не должно');
      }
      return this;
    }

    least(expected) {
      if (!this.__ok(this.__value >= expected)) {
        this.__fail(print(this.__value) + ' меньше ' + print(expected),
          print(this.__value) + ' не меньше ' + print(expected) + ', а не должно');
      }
      return this;
    }

    most(expected) {
      if (!this.__ok(this.__value <= expected)) {
        this.__fail(print(this.__value) + ' больше ' + print(expected),
          print(this.__value) + ' не больше ' + print(expected) + ', а не должно');
      }
      return this;
    }

    oneOf(expected) {
      const list = Array.isArray(expected) ? expected : [];
      let found = false;
      for (let i = 0; i < list.length; i++) if (equal(list[i], this.__value)) found = true;
      if (!this.__ok(found)) {
        this.__fail(print(this.__value) + ' не одно из ' + print(list),
          print(this.__value) + ' одно из ' + print(list) + ', а не должно');
      }
      return this;
    }
  }

  const readingWords = ['to', 'be', 'been', 'is', 'that', 'which', 'and', 'has', 'have', 'with', 'at', 'of', 'same', 'but', 'does'];
  readingWords.forEach((word) => {
    Object.defineProperty(Assertion.prototype, word, { get() { return this; } });
  });
  Object.defineProperty(Assertion.prototype, 'not', {
    get() { return new Assertion(this.__value, !this.__negated); },
  });

  // `true`, `empty` and the rest are properties in chai and in Postman, so they are properties here:
  // a script written there keeps working, and one written here reads the same way.
  const worth = (name, check) => {
    Object.defineProperty(Assertion.prototype, name, {
      get() {
        if (!this.__ok(check(this.__value))) {
          this.__fail('ожидалось ' + name + ', получено ' + print(this.__value),
            'значение ' + name + ', а не должно');
        }
        return this;
      },
    });
  };
  worth('true', (value) => value === true);
  worth('false', (value) => value === false);
  worth('null', (value) => value === null);
  worth('undefined', (value) => value === undefined);
  worth('ok', (value) => Boolean(value));
  worth('exist', (value) => value !== null && value !== undefined);
  worth('empty', (value) => {
    if (typeof value === 'string' || Array.isArray(value)) return value.length === 0;
    if (value !== null && typeof value === 'object') return Object.keys(value).length === 0;
    return false;
  });

  const scope = (name) => ({
    get: (key) => {
      const value = bridge.get(name, text(key));
      return value === undefined ? undefined : value;
    },
    set: (key, value) => { bridge.set(name, text(key), text(value)); },
    has: (key) => bridge.get(name, text(key)) !== undefined,
  });

  const pm = {
    variables: scope('variables'),
    environment: scope('environment'),
    globals: scope('globals'),
    test: (name, callback) => bridge.test(text(name), callback),
    expect: (value) => new Assertion(value, false),
    execution: { skipRequest: () => bridge.skip() },
    // The address is a string here, not Postman's Url object: a script reads and writes the whole
    // address, and the query is edited in the window. `pm.request.url.toString()` still works.
    request: (() => {
      const given = bridge.request;
      return {
        method: given.method,
        url: given.url,
        body: given.body,
        headers: headerList(given.headers, true),
      };
    })(),
  };

  const answer = bridge.response;
  if (answer) {
    pm.response = {
      code: answer.code,
      status: answer.status,
      responseTime: answer.responseTime,
      headers: headerList(answer.headers, false),
      text: () => answer.body,
      json: () => {
        try {
          return JSON.parse(answer.body);
        } catch (error) {
          throw new Error('тело ответа не разбирается как JSON: ' + error.message);
        }
      },
    };
  }

  const console = {};
  ['log', 'info', 'warn', 'error', 'debug'].forEach((level) => {
    console[level] = (...args) => bridge.log(level, args.map(print).join(' '));
  });

  globalThis.pm = pm;
  globalThis.console = console;
  delete globalThis.__bridge;
})(__bridge);
