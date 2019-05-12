(function() {
    function PVC() {
        this.namespace = "";
        this.names = [];
    }

    PVC.prototype.onObjectSelected = function(obj) {
        if (sprintf("%T", obj).split(".")[1] == "Pod") {
            this.names = [];
            obj.Spec.Volumes.forEach(function(vol) {
                if (vol.PersistentVolumeClaim != null) {
                    this.names.push(vol.PersistentVolumeClaim.ClaimName)
                }
            }.bind(this))

            if (!this.names.length) {
                return null
            }

            this.namespace = obj.Namespace

            return {"Label": "Persistent Volume Claims", "Callback": this.actionCallback.bind(this)}
        }

        return null
    }

    PVC.prototype.actionCallback = function() {
        var name = this.names[0];

        if (this.names.length > 1) {
            // Displays a list dialog with the string array, returning the
            // user-selected one
            name = kd.PickFrom("Persistent Volume Claims", this.names)
        }

        // Client holds the k8s.Client object
        var secret = kd.Client().CoreV1().PersistentVolumeClaims(this.namespace).Get(name, {})

        // Display can show plain text, or a k8s object
        kd.Display(secret)
    }

    PVC.prototype.update = function(c, obj) {
        c.CoreV1().PersistentVolumeClaims(obj.Namespace).Update(obj)
    }

    PVC.prototype.del = function(c, obj, opts) {
        c.CoreV1().PersistentVolumeClaims(obj.Namespace).Delete(obj.Name, opts)
    }

    PVC.prototype.summary = function(obj) {
        return sprintf(
            "[skyblue::b]Status:[white::-] %s\n" +
            "[skyblue::b]Volume:[white::-] %s\n" +
            "[skyblue::b]Access Modes:[white::-] %s\n" +
            "[skyblue::b]Storage Class:[white::-] %s\n",
            obj.Status.Phase,
            obj.Spec.VolumeName,
            obj.Status.AccessModes.join(", "),
            derefString(obj.Spec.StorageClassName)
        )
    }

    var pvc = new PVC()

    // Callback for when an object is selected in the tree
    // Return null if no action is to be taken.
    // Return ["Action name", callback] otherwise.
    kd.RegisterActionOnObjectSelected(pvc.onObjectSelected.bind(pvc))

    // Register callbacks that deal with object operation of a certain type
    kd.RegisterControllerOperator("PersistentVolumeClaim", {
        "Update": pvc.update.bind(pvc),
        "Delete": pvc.del.bind(pvc)
    })

    // Register callbacks that deal with object mutation of a certain type
    kd.RegisterObjectSummaryProvider("PersistentVolumeClaim", pvc.summary.bind(pvc))
})()
