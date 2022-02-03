<template>
  <div v-infinite-scroll="loadMore" class="p-page p-page-subjects" style="user-select: none"
       :infinite-scroll-disabled="scrollDisabled" :infinite-scroll-distance="1200"
       :infinite-scroll-listen-for-event="'scrollRefresh'">

    <v-form ref="form" class="p-people-search" lazy-validation dense @submit.prevent="updateQuery">
      <v-toolbar dense flat class="page-toolbar" color="secondary-light pa-0">
        <v-text-field id="search"
                      v-model="filter.q"
                      class="input-search background-inherit elevation-0"
                      solo hide-details
                      :label="$gettext('Search')"
                      prepend-inner-icon="search"
                      browser-autocomplete="off"
                      clearable overflow
                      color="secondary-dark"
                      @click:clear="clearQuery"
                      @keyup.enter.native="updateQuery"
        ></v-text-field>

        <v-divider vertical></v-divider>

        <v-btn icon overflow flat depressed color="secondary-dark" class="action-reload" :title="$gettext('Reload')" @click.stop="refresh">
          <v-icon>refresh</v-icon>
        </v-btn>

        <v-btn v-if="!filter.hidden" icon class="action-show-hidden" :title="$gettext('Show hidden')" @click.stop="onShowHidden">
          <v-icon>visibility</v-icon>
        </v-btn>
        <v-btn v-else icon class="action-exclude-hidden" :title="$gettext('Exclude hidden')" @click.stop="onExcludeHidden">
          <v-icon>visibility_off</v-icon>
        </v-btn>
      </v-toolbar>
    </v-form>

    <sublist :results="results" :loading="loading"/>
    <p-sponsor-dialog :show="dialog.sponsor" @close="dialog.sponsor = false"></p-sponsor-dialog>
  </div>
</template>

<script>
import Subject from "model/subject";
import Event from "pubsub-js";
import RestModel from "model/rest";
import {MaxItems} from "common/clipboard";
import Notify from "common/notify";
import {ClickLong, ClickShort, Input, InputInvalid} from "common/input";
import Sublist from "./sublist.vue"

export default {
  name: 'PPageSubjects',
  props: {
    staticFilter: Object,
    active: Boolean,
  },
  components: {
    'sublist': Sublist,
  },
  data() {
    const query = this.$route.query;
    const routeName = this.$route.name;
    const q = query['q'] ? query['q'] : '';
    const hidden = query['hidden'] ? query['hidden'] : '';
    const order = this.sortOrder();
    const filter = {q, hidden, order};
    const settings = {};

    return {
      view: 'all',
      config: this.$config.values,
      subscriptions: [],
      listen: false,
      dirty: false,
      results: [],
      scrollDisabled: true,
      loading: true,
      batchSize: Subject.batchSize(),
      offset: 0,
      page: 0,
      selection: [],
      settings: settings,
      filter: filter,
      lastFilter: {},
      routeName: routeName,
      input: new Input(),
      lastId: "",
      dialog: {
        sponsor: false,
      },
      merge: {
        subj1: null,
        subj2: null,
        show: false,
      },
    };
  },
  computed: {
    readonly: function() {
      return this.busy || this.loading;
    },
  },
  watch: {
    '$route'() {
      // Tab inactive?
      if (!this.active) {
        // Ignore event.
        return;
      }

      const query = this.$route.query;

      this.filter.q = query["q"] ? query["q"] : "";
      this.filter.hidden = query["hidden"] ? query["hidden"] : "";
      this.filter.order = this.sortOrder();
      this.routeName = this.$route.name;

      this.search();
    }
  },
  created() {
    this.search();

    this.subscriptions.push(Event.subscribe("subjects", (ev, data) => this.onUpdate(ev, data)));

    this.subscriptions.push(Event.subscribe("touchmove.top", () => this.refresh()));
    this.subscriptions.push(Event.subscribe("touchmove.bottom", () => this.loadMore()));
  },
  destroyed() {
    for (let i = 0; i < this.subscriptions.length; i++) {
      Event.unsubscribe(this.subscriptions[i]);
    }
  },
  methods: {
    onSave(m) {
      if (!m.Name || m.Name.trim() === "") {
        // Refuse to save empty name.
        return;
      }

      const existing = this.$config.getPerson(m.Name);

      if (!existing) {
        this.busy = true;
        m.update().finally(() => {
          this.busy = false;
        });
      } else if (existing.UID !== m.UID) {
        this.merge.subj1 = m;
        this.merge.subj2 = existing;
        this.merge.show = true;
      }
    },
    onCancelMerge() {
      this.merge.subj1.Name = this.merge.subj1.originalValue("Name");
      this.merge.show = false;
      this.merge.subj1 = null;
      this.merge.subj2 = null;
    },
    onMerge() {
      this.busy = true;
      this.merge.show = false;
      this.$notify.blockUI();
      this.merge.subj1.update().finally(() => {
        this.busy = false;
        this.merge.subj1 = null;
        this.merge.subj2 = null;
        this.$notify.unblockUI();
        this.refresh();
      });
    },
    searchCount() {
      const offset = parseInt(window.localStorage.getItem("subjects_offset"));

      if (this.offset > 0 || !offset) {
        return this.batchSize;
      }

      return offset + this.batchSize;
    },
    sortOrder() {
      return "relevance";
    },
    setOffset(offset) {
      this.offset = offset;
      window.localStorage.setItem("subjects_offset", offset);
    },
    onShowHidden() {
      this.showHidden("yes");
    },
    onExcludeHidden() {
      this.showHidden("");
    },
    showHidden(value) {
      this.$earlyAccess().then(() => {
        this.filter.hidden = value;
        this.updateQuery();
      }).catch(() => {
        this.dialog.sponsor = true;
      });
    },
    onToggleHidden(ev, index) {
      const inputType = this.input.eval(ev, index);

      if (inputType !== ClickShort) {
        return;
      }

      this.$earlyAccess().then(() => {
        this.toggleHidden(this.results[index]);
      }).catch(() => {
        this.dialog.sponsor = true;
      });
    },
    toggleHidden(model) {
      if (!model) {
        return;
      }
      this.busy = true;
      model.toggleHidden().finally(() => {
        this.busy = false;
      });
    },
    clearQuery() {
      this.filter.q = '';
      this.updateQuery();
    },
    loadMore() {
      if (this.scrollDisabled || !this.active) {
        return;
      }

      this.scrollDisabled = true;
      this.listen = false;

      const count = this.dirty ? (this.page + 2) * this.batchSize : this.batchSize;
      const offset = this.dirty ? 0 : this.offset;

      const params = {
        count: count,
        offset: offset,
      };

      Object.assign(params, this.lastFilter);

      if (this.staticFilter) {
        Object.assign(params, this.staticFilter);
      }

      Subject.search(params).then(resp => {
        this.results = this.dirty ? resp.models : this.results.concat(resp.models);

        this.scrollDisabled = (resp.count < resp.limit);

        if (this.scrollDisabled) {
          this.setOffset(resp.offset);
          if (this.results.length > 1) {
            this.$notify.info(this.$gettextInterpolate(this.$gettext("All %{n} people loaded"), {n: this.results.length}));
          }
        } else {
          this.setOffset(resp.offset + resp.limit);
          this.page++;

          this.$nextTick(() => {
            if (this.$root.$el.clientHeight <= window.document.documentElement.clientHeight + 300) {
              this.$emit("scrollRefresh");
            }
          });
        }
      }).catch(() => {
        this.scrollDisabled = false;
      }).finally(() => {
        this.dirty = false;
        this.loading = false;
        this.listen = true;
      });
    },
    updateQuery() {
      this.filter.q = this.filter.q.trim();

      const query = {
        view: this.settings.view
      };

      Object.assign(query, this.filter);

      for (let key in query) {
        if (query[key] === undefined || !query[key]) {
          delete query[key];
        }
      }

      if (JSON.stringify(this.$route.query) === JSON.stringify(query)) {
        return;
      }

      this.$router.replace({query: query});
    },
    searchParams() {
      const params = {
        count: this.searchCount(),
        offset: this.offset,
      };

      Object.assign(params, this.filter);

      if (this.staticFilter) {
        Object.assign(params, this.staticFilter);
      }

      return params;
    },
    refresh() {
      if (this.loading || !this.active) {
        return;
      }

      this.loading = true;
      this.page = 0;
      this.dirty = true;
      this.scrollDisabled = false;

      this.loadMore();
    },
    search() {
      this.scrollDisabled = true;

      // Don't query the same data more than once
      if (JSON.stringify(this.lastFilter) === JSON.stringify(this.filter)) {
        this.$nextTick(() => this.$emit("scrollRefresh"));
        return;
      }

      Object.assign(this.lastFilter, this.filter);

      this.offset = 0;
      this.page = 0;
      this.loading = true;
      this.listen = false;

      const params = this.searchParams();

      Subject.search(params).then(resp => {
        this.offset = resp.limit;
        this.results = resp.models;

        this.scrollDisabled = (resp.count < resp.limit);

        if (this.scrollDisabled) {
          if (!this.results.length) {
            this.$notify.warn(this.$gettext("No people found"));
          } else if (this.results.length === 1) {
            this.$notify.info(this.$gettext("One person found"));
          } else {
            this.$notify.info(this.$gettextInterpolate(this.$gettext("%{n} people found"), {n: this.results.length}));
          }
        } else {
          this.$notify.info(this.$gettext('More than 20 people found'));

          this.$nextTick(() => {
            if (this.$root.$el.clientHeight <= window.document.documentElement.clientHeight + 300) {
              this.$emit("scrollRefresh");
            }
          });
        }
      }).finally(() => {
        this.dirty = false;
        this.loading = false;
        this.listen = true;
      });
    },
    onUpdate(ev, data) {
      if (!this.listen) return;

      if (!data || !data.entities || !Array.isArray(data.entities)) {
        return;
      }

      const type = ev.split('.')[1];

      switch (type) {
        case 'updated':
          for (let i = 0; i < data.entities.length; i++) {
            const values = data.entities[i];
            const model = this.results.find((m) => m.UID === values.UID);

            if (model) {
              for (let key in values) {
                if (values.hasOwnProperty(key) && values[key] != null && typeof values[key] !== "object") {
                  model[key] = values[key];
                }
              }
            }
          }
          break;
        case 'deleted':
          this.dirty = true;

          for (let i = 0; i < data.entities.length; i++) {
            const uid = data.entities[i];
            const index = this.results.findIndex((m) => m.UID === uid);

            if (index >= 0) {
              this.results.splice(index, 1);
            }

            this.removeSelection(uid);
          }

          break;
        case 'created':
          this.dirty = true;
          break;
        default:
          console.warn("unexpected event type", ev);
      }
    }
  },
};
</script>
