(function() {
    function Secrets() {
        this.namespace = "";
        this.names = [];
        this.secrets = [];
    }

    Secrets.prototype.onObjectSelected = function(obj) {
        if (sprintf("%T", obj).split(".")[1] == "Pod") {
            this.names = [];
            obj.Spec.Volumes.forEach(function(vol) {
                if (vol.Secret != null) {
                    this.names.push(vol.Secret.SecretName)
                }
            }.bind(this))

            if (!this.names.length) {
                return null
            }

            this.namespace = obj.Namespace

            return {"Label": "Secret", "Callback": this.actionCallback.bind(this)}
        } else if (sprintf("%T", obj).split(".")[1] == "Secret") {
            this.secrets = obj.Data

            if (!Object.keys(this.secrets).length) {
                return null
            }

            return {"Label": "Secret data", "Callback": this.secretKeysActionCallback.bind(this)}
        }

        return null
    }

    Secrets.prototype.actionCallback = function() {
        var name = this.names[0];

        if (this.names.length > 1) {
            // Displays a list dialog with the string array, returning the
            // user-selected one
            name = kd.PickFrom("Secrets", this.names)
        }

        // Client holds the k8s.Client object
        var secret = kd.Client().CoreV1().Secrets(this.namespace).Get(name, {})

        // Display can show plain text, or a k8s object
        kd.Display(secret)
    }

    Secrets.prototype.secretKeysActionCallback = function() {
        var keys = Object.keys(this.secrets)
        var key = keys[0];

        logf("Secret keys: %v, secrets: %v", keys, this.secrets)
        if (keys.length > 1) {
            key = kd.PickFrom("Secret keys", keys)
        }

        // Display can show plain text, or a k8s object
        kd.Display(this.secrets[key])
    }

    Secrets.prototype.update = function(c, obj) {
        c.CoreV1().Secrets(obj.Namespace).Update(obj)
    }

    Secrets.prototype.del = function(c, obj, opts) {
        c.CoreV1().Secrets(obj.Namespace).Delete(obj.Name, opts)
    }

    Secrets.prototype.summary = function(obj) {
        return sprintf(
            "[skyblue::b]Type:[white::-] %s\n[skyblue::b]Data:[white::-] %d\n",
            obj.Type,
            Object.keys(obj.Data).length
        )
    }

    var secrets = new Secrets()

    // Callback for when an object is selected in the tree
    // Return null if no action is to be taken.
    // Return ["Action name", callback] otherwise.
    kd.RegisterActionOnObjectSelected(secrets.onObjectSelected.bind(secrets))

    // Register callbacks that deal with object operation of a certain type
    kd.RegisterControllerOperator("Secret", {
        "Update": secrets.update.bind(secrets),
        "Delete": secrets.del.bind(secrets)
    })

    // Register callbacks that deal with object mutation of a certain type
    kd.RegisterObjectSummaryProvider("Secret", secrets.summary.bind(secrets))
})()
